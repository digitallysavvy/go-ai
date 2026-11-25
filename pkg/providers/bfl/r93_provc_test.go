package bfl

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93provcSampleIsZodOutput: TS bflPollSchema and bflVideoResultSchema
// declare result.sample as z.url(), so the image is downloaded from (and the
// video reported at) the zod output: JS-trimmed, tab / LF / CR removed.
func TestR93provcSampleIsZodOutput(t *testing.T) {
	var mu sync.Mutex
	var downloaded []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = io.WriteString(w, `{"id":"r1","polling_url":"http://`+r.Host+`/poll"}`)
		case "/poll":
			_, _ = io.WriteString(w, `{"status":"Ready","result":{"sample":" http://`+r.Host+`/im\tg.png\n"}}`)
		case "/img.png":
			mu.Lock()
			downloaded = append(downloaded, r.URL.Path)
			mu.Unlock()
			_, _ = io.WriteString(w, "png")
		case "/get_result":
			_, _ = io.WriteString(w, `{"status":"Ready","result":{"sample":" https://cdn.example.com/v\n.mp4 "}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	res, err := NewImageModel(p, "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:          "x",
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	mu.Lock()
	got := strings.Join(downloaded, ",")
	mu.Unlock()
	if got != "/img.png" || res.URL != server.URL+"/img.png" {
		t.Errorf("downloaded %q, URL %q", got, res.URL)
	}

	op, _ := json.Marshal(bflVideoOperation{RequestID: "v1", PollingURL: server.URL + "/get_result"})
	status, err := NewVideoModel(p, VideoModelFlux3Video).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus: %v", err)
	}
	want := "https://cdn.example.com/v.mp4"
	videos := status.ProviderMetadata["blackForestLabs"].(map[string]interface{})["videos"].([]map[string]interface{})
	if len(status.Videos) != 1 || status.Videos[0].URL != want || videos[0]["videoUrl"] != want {
		t.Errorf("videos = %+v, metadata %v", status.Videos, videos)
	}
}

// TestR93provcPollingURLIsWHATWGHref: TS polls url.toString() of new
// URL(pollingUrl) after searchParams.set('id') (node-verified outputs); an
// upper-case scheme is still the absolute URL, not a path under the base URL.
func TestR93provcPollingURLIsWHATWGHref(t *testing.T) {
	cases := map[string]string{
		"HTTPS://API.bfl.ai:443/v1/get_result":          "https://api.bfl.ai/v1/get_result?id=r+1",
		"https://api.bfl.ai/a/../get_result?id=x y":     "https://api.bfl.ai/get_result?id=x%20y",
		"https://api.bfl.ai/r?q=a b":                    "https://api.bfl.ai/r?q=a+b&id=r+1",
		"https://api.us1.bfl.ai/v1/get_result?id=abc#f": "https://api.us1.bfl.ai/v1/get_result?id=abc#f",
	}
	for in, want := range cases {
		got, err := ensurePollingURLHasID(in, "r 1")
		if err != nil || got != want {
			t.Errorf("ensurePollingURLHasID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	var mu sync.Mutex
	var polled []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = io.WriteString(w, `{"id":"r1","polling_url":"HTTP://`+r.Host+`/poll"}`)
		case "/poll":
			mu.Lock()
			polled = append(polled, r.URL.RequestURI())
			mu.Unlock()
			_, _ = io.WriteString(w, `{"status":"Error"}`)
		default:
			mu.Lock()
			polled = append(polled, r.URL.RequestURI())
			mu.Unlock()
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, err := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil || err.Error() != "Black Forest Labs generation failed." {
		t.Errorf("err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(polled, ",") != "/poll?id=r1" {
		t.Errorf("polled %q, want /poll?id=r1", polled)
	}
}

// TestR93provcVideoPollNetworkFailureIsConnectError: TS doStatus polls with
// getFromApi, whose handleFetchError turns a failed connection into the
// retryable "Cannot connect to API" APICallError. Go returned the
// DownloadError fileutil wraps it in.
func TestR93provcVideoPollNetworkFailureIsConnectError(t *testing.T) {
	old := downloadTransport
	downloadTransport = func() http.RoundTripper {
		return &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: r92integBFLDialTimeout{}}
		}}
	}
	t.Cleanup(func() { downloadTransport = old })

	op, _ := json.Marshal(bflVideoOperation{RequestID: "v1", PollingURL: "https://poll.example.com/get_result"})
	_, err := NewVideoModel(New(Config{APIKey: "k"}), VideoModelFlux3Video).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}

	// The caller's abort stays an abort.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewVideoModel(New(Config{APIKey: "k"}), VideoModelFlux3Video).DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil || !strings.Contains(err.Error(), "context canceled") || strings.HasPrefix(err.Error(), "Cannot connect") {
		t.Errorf("aborted err = %v", err)
	}
}
