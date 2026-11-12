package replicate

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcNaNDurationIsFalsy (r92 provc-13): TS `if (options.duration)`
// skips a NaN duration.
func TestR92provcNaNDurationIsFalsy(t *testing.T) {
	nan := math.NaN()
	input, err := (&VideoModel{modelID: "luma/ray"}).buildInput(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := input["duration"]; ok {
		t.Errorf("input = %v", input)
	}
}

// TestR92provcVideoBodyKeyOrder (r92 provc-17): the prediction body follows
// the TS literal order ({input, version, webhook, webhook_events_filter},
// input in buildInput order), not Go's sorted map keys.
func TestR92provcVideoBodyKeyOrder(t *testing.T) {
	var mu sync.Mutex
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p1","status":"starting","urls":{"get":"`+"http://"+r.Host+`/predictions/p1"}}`)
	}))
	defer srv.Close()
	d, seed := 5.0, 7
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "minimax/video-01:abc")
	if _, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", AspectRatio: "16:9", Duration: &d, Seed: &seed,
			ProviderOptions: map[string]interface{}{"replicate": map[string]interface{}{"guidance_scale": 3, "zeta": 1}}},
		WebhookURL: "https://example.com/hook",
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := `{"input":{"prompt":"p","aspect_ratio":"16:9","duration":5,"seed":7,"guidance_scale":3,"zeta":1},"version":"abc","webhook":"https://example.com/hook","webhook_events_filter":["completed"]}`
	if raw != want {
		t.Errorf("body = %s\nwant   %s", raw, want)
	}
}
