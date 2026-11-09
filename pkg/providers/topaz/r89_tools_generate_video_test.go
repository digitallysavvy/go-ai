// Uses the package's videoTestServer (video_model_test.go).
package topaz

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS TopazVideoModel has no doGenerate: generateVideo runs the core
// start/status flow, so the express start carries one aisdk_vid_
// idempotency key and a 500 on the status GET is retried without a second,
// billable start.
func TestR89toolsTopazGenerateVideoUsesCoreStartStatusFlow(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	statusCalls := 0
	inner := ts.server.Config.Handler
	ts.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			ts.mu.Lock()
			statusCalls++
			first := statusCalls == 1
			ts.mu.Unlock()
			if first {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"busy"}`))
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
	opts := defaultStartOptions()
	ref := opts.InputReferences[0]
	_, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		// The model's 200 ms poll timeout is shorter than the core retry
		// backoff after the status 500, so this model polls for up to 10 s.
		Model:           NewVideoModel(New(Config{APIKey: "test-key", BaseURL: ts.server.URL, VideoPollIntervalMillis: 1, VideoPollTimeoutMillis: 10000}), VideoModelStarlightPrecise26),
		Prompt:          ai.VideoPrompt{},
		InputReferences: []ai.VideoReferenceInput{{Data: ai.VideoPromptImage{Data: ref.Data, MediaType: ref.MediaType}}},
		ProviderOptions: opts.ProviderOptions,
		Download:        func(context.Context, string) ([]byte, error) { return []byte("video"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.expressCalls != 1 {
		t.Fatalf("express starts = %d, want 1", ts.expressCalls)
	}
	if key := ts.expressHeaders.Get("Idempotency-Key"); !strings.HasPrefix(key, "aisdk_vid_") {
		t.Fatalf("idempotency-key = %q", key)
	}
	if statusCalls != 2 {
		t.Fatalf("status calls = %d, want 2", statusCalls)
	}
}
