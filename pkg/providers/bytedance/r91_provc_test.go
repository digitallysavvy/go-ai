package bytedance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r91provcStatusServer answers the status poll with body and records the
// Authorization header.
func r91provcStatusServer(t *testing.T, body string, auth *string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Add("X-Test", "a")
		w.Header().Add("X-Test", "b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r91provcStatus(t *testing.T, p *Provider) (*provider.VideoModelV3OperationStatusResult, error) {
	t.Helper()
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t1"})
	return newVideoModel(p, "seedance").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
}

// TestR91provcStatusUsage (provc-13): a fractional completion_tokens decodes
// (TS z.number()), and an absent usage adds no usage key.
func TestR91provcStatusUsage(t *testing.T) {
	var mu sync.Mutex
	var auth string
	srv := r91provcStatusServer(t, `{"id":"t1","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"},"usage":{"completion_tokens":12.5,"extra":1}}`, &auth, &mu)
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	status, err := r91provcStatus(t, p)
	if err != nil {
		t.Fatalf("DoStatus: %v", err)
	}
	meta := status.ProviderMetadata["bytedance"].(map[string]interface{})
	if b, _ := json.Marshal(meta["usage"]); string(b) != `{"completion_tokens":12.5}` {
		t.Errorf("usage = %s", b)
	}
	if got := status.Response.Headers["X-Test"]; got != "a, b" {
		t.Errorf("X-Test = %q, want \"a, b\" (provc-06)", got)
	}

	srv2 := r91provcStatusServer(t, `{"id":"t1","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"}}`, &auth, &mu)
	p2, _ := New(Config{APIKey: "k", BaseURL: srv2.URL})
	status, err = r91provcStatus(t, p2)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := status.ProviderMetadata["bytedance"].(map[string]interface{})["usage"]; ok {
		t.Errorf("usage = %#v, want no key", v)
	}
}

// TestR91provcFractionalPollOptions (provc-13): fractional poll options are
// valid TS numbers and must not fail the option decode.
func TestR91provcFractionalPollOptions(t *testing.T) {
	opts, err := extractProviderOptions(map[string]interface{}{"bytedance": map[string]interface{}{"pollIntervalMs": 10.5, "pollTimeoutMs": 2000.2}})
	if err != nil {
		t.Fatalf("extractProviderOptions: %v", err)
	}
	if opts.PollIntervalMs == nil || *opts.PollIntervalMs != 11 || opts.PollTimeoutMs == nil || *opts.PollTimeoutMs != 2001 {
		t.Errorf("poll options = %v %v", opts.PollIntervalMs, opts.PollTimeoutMs)
	}
}

// TestR91provcStatusKeyPerRequest (provc-14): the poll resolves the key per
// request (TS loadApiKey in getHeaders): ARK_API_KEY set after New is used,
// and no key at all is a LoadAPIKeyError before any request.
func TestR91provcStatusKeyPerRequest(t *testing.T) {
	t.Setenv("BYTEDANCE_API_KEY", "")
	t.Setenv("ARK_API_KEY", "")
	var mu sync.Mutex
	auth := "unset"
	srv := r91provcStatusServer(t, `{"id":"t1","status":"running"}`, &auth, &mu)
	p, _ := New(Config{BaseURL: srv.URL})

	_, err := r91provcStatus(t, p)
	var keyErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("err = %T %v, want LoadAPIKeyError", err, err)
	}
	mu.Lock()
	if auth != "unset" {
		t.Errorf("a request was sent without a key (Authorization %q)", auth)
	}
	mu.Unlock()

	t.Setenv("ARK_API_KEY", "env-key")
	if _, err := r91provcStatus(t, p); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if auth != "Bearer env-key" {
		t.Errorf("Authorization = %q, want Bearer env-key", auth)
	}
}

// TestR91provcHTTPErrorText (provc-12): the error text is the TS
// APICallError message alone.
func TestR91provcHTTPErrorText(t *testing.T) {
	err := newHTTPError(400, nil, []byte(`{"error":{"message":"bad input"}}`))
	if err.Error() != "bad input" {
		t.Errorf("Error() = %q, want \"bad input\"", err.Error())
	}
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 400 || err.Code != 400 {
		t.Errorf("unwrap = %#v code %d", perr, err.Code)
	}
}

// TestR91provcCreateBodyOrder (provc-07): the create body keeps the TS
// buildRequestBody insertion order, passthrough keys, then callback_url.
func TestR91provcCreateBodyOrder(t *testing.T) {
	var mu sync.Mutex
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"t1"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	d := 5.0
	seed := 3
	audio := true
	_, err := newVideoModel(p, "seedance").DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x", AspectRatio: "16:9", Duration: &d, Seed: &seed, Resolution: "1920x1080", GenerateAudio: &audio,
			ProviderOptions: map[string]interface{}{"bytedance": map[string]interface{}{"watermark": false, "draft": true, "frames": 9}},
		},
		WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := `{"model":"seedance","content":[{"type":"text","text":"x"}],"ratio":"16:9","duration":5,"seed":3,"resolution":"1080p","generate_audio":true,"watermark":false,"draft":true,"frames":9,"callback_url":"https://example.com/hook"}`; raw != want {
		t.Errorf("body = %s\nwant %s", raw, want)
	}
}
