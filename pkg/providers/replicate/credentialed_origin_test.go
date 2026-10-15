package replicate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
