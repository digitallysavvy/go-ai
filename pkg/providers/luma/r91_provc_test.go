package luma

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR91provcBodyOrder (provc-07): the generation body is TS's {prompt,
// aspect_ratio, model, ...editingOptions, ...providerRequestOptions}, with
// character identities in insertion order.
func TestR91provcBodyOrder(t *testing.T) {
	var mu sync.Mutex
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"detail":"stop"}`)
	}))
	defer server.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelPhoton1)
	_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p", AspectRatio: "16:9",
		Files: []provider.ImageFile{{Type: "url", URL: "https://example.com/1.png"}, {Type: "url", URL: "https://example.com/2.png"}},
		ProviderOptions: map[string]interface{}{"luma": map[string]interface{}{
			"referenceType": "character",
			"images":        []interface{}{map[string]interface{}{"id": "zed"}, map[string]interface{}{"id": "abe"}},
			"zeta":          1,
		}},
	})
	mu.Lock()
	defer mu.Unlock()
	if want := `{"prompt":"p","aspect_ratio":"16:9","model":"photon-1","character":{"zed":{"images":["https://example.com/1.png"]},"abe":{"images":["https://example.com/2.png"]}},"zeta":1}`; raw != want {
		t.Errorf("body = %s\nwant %s", raw, want)
	}
}

// TestR91provcHeadersJoin (provc-06): repeated headers join with ", ".
func TestR91provcHeadersJoin(t *testing.T) {
	h := flattenHeaders(map[string][]string{"X-Test": {"a", "b"}})
	if h["X-Test"] != "a, b" {
		t.Errorf("X-Test = %q", h["X-Test"])
	}
}

// TestR91provcTypedOptionLiterals (typed-option ruling).
func TestR91provcTypedOptionLiterals(t *testing.T) {
	opts := parseImageModelOptions(map[string]interface{}{"luma": map[string]interface{}{
		"images":             []map[string]interface{}{{"weight": float32(0.5), "id": "a"}},
		"pollIntervalMillis": int64(100),
	}})
	if opts == nil || len(opts.Images) != 1 || opts.Images[0].Weight == nil || *opts.Images[0].Weight != 0.5 || opts.PollIntervalMillis == nil || *opts.PollIntervalMillis != 100 {
		t.Fatalf("options = %+v", opts)
	}
}
