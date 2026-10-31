package replicate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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

// Behavioral: a same-origin status URL gets the provider token, and a
// response-derived URL on another origin never reaches the credentialed
// transport. (v0.5.3 swapped the download transport through a test hook; on
// v0.6.0 same-origin requests use the provider client, so the base server
// observes them directly.)
func TestReplicateVideoDoStatusAuthHeaderByOrigin(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p","status":"processing","urls":{"get":"http://` + r.Host + `/v1/predictions/p"}}`))
	}))
	defer base.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin was requested with Authorization %q", r.Header.Get("Authorization"))
	}))
	defer other.Close()

	model := NewVideoModel(New(Config{APIKey: "secret", BaseURL: base.URL + "/v1"}), "minimax/video-01")
	op, _ := json.Marshal(replicateVideoOperation{GetURL: base.URL + "/v1/predictions/p"})
	if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op}); err != nil {
		t.Fatal(err)
	}
	op, _ = json.Marshal(replicateVideoOperation{GetURL: other.URL + "/predictions/p"})
	_, _ = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})

	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 1 || auths[0] != "Bearer secret" {
		t.Fatalf("base requests Authorization = %q, want one Bearer secret", auths)
	}
}
