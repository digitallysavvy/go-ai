package replicate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR91provcImageBodyOrder (provc-07): the prediction body is TS's
// {input: {prompt, aspect_ratio, size, seed, num_outputs, images, mask,
// ...options}, version}.
func TestR91provcImageBodyOrder(t *testing.T) {
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
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "owner/model:v1")
	n, seed := 2, 4
	_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p", AspectRatio: "1:1", Size: "64x64", Seed: &seed, N: &n,
		Files:           []provider.ImageFile{{Type: "url", URL: "https://example.com/i.png"}},
		Mask:            &provider.ImageFile{Type: "url", URL: "https://example.com/m.png"},
		ProviderOptions: map[string]interface{}{"replicate": map[string]interface{}{"guidance": 3, "alpha": "x"}},
	})
	mu.Lock()
	defer mu.Unlock()
	if want := `{"input":{"prompt":"p","aspect_ratio":"1:1","size":"64x64","seed":4,"num_outputs":2,"image":"https://example.com/i.png","mask":"https://example.com/m.png","alpha":"x","guidance":3},"version":"v1"}`; raw != want {
		t.Errorf("body = %s\nwant %s", raw, want)
	}
}

// TestR91provcHeadersJoin (provc-06): repeated headers join with ", ".
func TestR91provcHeadersJoin(t *testing.T) {
	if h := flattenReplicateHeaders(map[string][]string{"X-Test": {"a", "b"}}); h["X-Test"] != "a, b" {
		t.Errorf("X-Test = %q", h["X-Test"])
	}
}

// TestR91provcTypedOptionLiterals (typed-option ruling).
func TestR91provcTypedOptionLiterals(t *testing.T) {
	poll := extractImagePollOptions(&provider.ImageGenerateOptions{ProviderOptions: map[string]interface{}{
		"replicate": map[string]int64{"maxPollAttempts": 3, "pollIntervalMillis": 20},
	}})
	if poll.MaxPollAttempts == nil || *poll.MaxPollAttempts != 3 || poll.PollIntervalMillis == nil || *poll.PollIntervalMillis != 20 {
		t.Fatalf("poll = %+v", poll)
	}
	interval, timeout := (&VideoModel{}).VideoStartStatusPollDefaults(map[string]interface{}{"replicate": map[string]uint32{"pollIntervalMs": 50, "pollTimeoutMs": 900}})
	if interval != 50 || timeout != 900 {
		t.Errorf("video poll defaults = %d %d", interval, timeout)
	}
}
