package replicate

import (
	"context"
	"encoding/json"
	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The prediction urls.get comes from a response body; the API token must only
// go to the configured base URL's origin.
func TestReplicateCredentialedHeadersOnlySameOrigin(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://replicate.example/v1"})
	if h := p.credentialedHeaders("https://replicate.example/v1/predictions/1", map[string]string{"X": "1"}); h["authorization"] != "Token k" || h["X"] != "1" {
		t.Fatalf("same-origin headers = %v", h)
	}
	if h := p.credentialedHeaders("https://evil.example.net/predictions/1", map[string]string{"X": "1"}); h != nil {
		t.Fatalf("foreign origin must get no headers, got %v", h)
	}
	d := New(Config{APIKey: "k"})
	if d.credentialedHeaders("https://attacker.test/x", nil) != nil || d.credentialedHeaders("https://api.replicate.com/v1/predictions/1", nil) == nil {
		t.Fatal("default base URL origin handling wrong")
	}
}

// DoStatus must not send the API token to a foreign status URL.
func TestReplicateVideoDoStatusDoesNotLeakTokenToForeignURL(t *testing.T) {
	var foreignAuth string
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p","status":"processing"}`))
	}))
	defer foreign.Close()
	base := httptest.NewServer(http.NotFoundHandler())
	defer base.Close()

	model := NewVideoModel(New(Config{APIKey: "secret", BaseURL: base.URL}), "minimax/video-01")
	op, _ := json.Marshal(replicateVideoOperation{GetURL: foreign.URL + "/predictions/p"})
	_, _ = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if foreignAuth != "" {
		t.Fatalf("token leaked to foreign origin: %q", foreignAuth)
	}
}

// recordingTransport records every request and answers with a processing
// prediction.
type recordingTransport struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req.Clone(req.Context()))
	r.mu.Unlock()
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"p","status":"processing"}`)),
		Request:    req,
	}, nil
}

// Behavioral: the status request to a response-derived URL on a different
// origin must carry no Authorization header, while the same-origin one does.
// (The SSRF guard is untouched: only the transport is swapped to observe the
// request that would be sent.)
func TestReplicateVideoDoStatusAuthHeaderByOrigin(t *testing.T) {
	rec := &recordingTransport{}
	statusOptionsHook = func(o *fileutil.DownloadOptions) {
		o.Transport = rec
		o.URLValidator = nil
	}
	defer func() { statusOptionsHook = nil }()

	model := NewVideoModel(New(Config{APIKey: "secret", BaseURL: "https://replicate.example/v1"}), "minimax/video-01")
	for _, tc := range []struct {
		url      string
		wantAuth string
	}{
		{"https://replicate.example/v1/predictions/p", "Token secret"},
		{"https://evil.example.net/predictions/p", ""},
		{"http://replicate.example/v1/predictions/p", ""},
	} {
		op, _ := json.Marshal(replicateVideoOperation{GetURL: tc.url})
		if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op}); err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		rec.mu.Lock()
		got := rec.reqs[len(rec.reqs)-1]
		rec.mu.Unlock()
		if got.URL.String() != tc.url {
			t.Fatalf("requested %s, want %s", got.URL, tc.url)
		}
		if a := got.Header.Get("Authorization"); a != tc.wantAuth {
			t.Errorf("%s: Authorization = %q, want %q", tc.url, a, tc.wantAuth)
		}
	}
}
