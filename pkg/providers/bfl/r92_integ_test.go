package bfl

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type r92integBFLDialTimeout struct{}

func (r92integBFLDialTimeout) Error() string        { return "i/o timeout" }
func (r92integBFLDialTimeout) Timeout() bool        { return true }
func (r92integBFLDialTimeout) Temporary() bool      { return true }
func (r92integBFLDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

// TestR92integPollAndDownloadNetworkFailureIsConnectError: TS polls and
// downloads with getFromApi, whose handleFetchError turns a network failure
// (a dial timeout or a refused connection) into the retryable "Cannot
// connect to API" APICallError. Go returned the raw *url.Error.
func TestR92integPollAndDownloadNetworkFailureIsConnectError(t *testing.T) {
	old := downloadTransport
	downloadTransport = func() http.RoundTripper {
		return &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: r92integBFLDialTimeout{}}
		}}
	}
	t.Cleanup(func() { downloadTransport = old })

	im, err := New(Config{APIKey: "k"}).ImageModel("flux-pro-1.1")
	if err != nil {
		t.Fatal(err)
	}
	m := im.(*ImageModel)
	check := func(name string, err error) {
		t.Helper()
		r, ok := err.(interface{ IsRetryable() bool })
		if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
			t.Errorf("%s: %T %v, want the retryable Cannot connect to API error", name, err, err)
		}
	}
	_, err = m.getPollBody(context.Background(), "https://poll.example.com/get_result?id=1", nil)
	check("poll", err)
	_, _, err = m.downloadImage(context.Background(), "https://delivery.example.com/x.png", nil)
	check("download", err)
}

// TestR92integWebhookURLIsZodOutput (cr-r92-carry-to-provc-applied): TS
// sends the z.url() output of webhookUrl, JS-trimmed without tab, LF and CR
// (black-forest-labs-image-model-options.ts webhookUrl: z.url()).
func TestR92integWebhookURLIsZodOutput(t *testing.T) {
	out := extractBFLProviderOptions(map[string]interface{}{"blackForestLabs": map[string]interface{}{"webhookUrl": " https://x.dev/ho\tok\n"}})
	if out["webhook_url"] != "https://x.dev/hook" {
		t.Errorf("webhook_url = %q", out["webhook_url"])
	}
}

// TestR92integPollingURLIsZodOutput: TS bflSubmitSchema polling_url is
// z.url().nullish() and bflVideoSubmitSchema polling_url z.url(), so the
// poll goes to the zod output, not the raw string.
func TestR92integPollingURLIsZodOutput(t *testing.T) {
	old := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = old })
	var mu sync.Mutex
	var polled []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"r1","polling_url":" http://` + r.Host + `/poll\n"}`))
		case "/flux-3-video":
			_, _ = w.Write([]byte(`{"id":"v1","polling_url":" https://api.bfl.ai/v1/get_result\r\n"}`))
		case "/poll":
			mu.Lock()
			polled = append(polled, r.URL.RequestURI())
			mu.Unlock()
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"http://` + r.Host + `/img.png"}}`))
		case "/img.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	_, err := NewImageModel(p, "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:          "x",
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	mu.Lock()
	got := strings.Join(polled, ",")
	mu.Unlock()
	if got != "/poll?id=r1" {
		t.Errorf("polled %q, want /poll?id=r1", got)
	}

	start, err := NewVideoModel(p, VideoModelFlux3Video).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	if !strings.Contains(string(start.Operation), `"pollingUrl":"https://api.bfl.ai/v1/get_result"`) {
		t.Errorf("operation = %s", start.Operation)
	}
}
