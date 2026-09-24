package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports TS xai-video-model.test.ts request-id encoding cases (7de3612): the
// provider-returned request_id is path-encoded in the credentialed status URL.
func TestVideoModel_EncodesProviderRequestID(t *testing.T) {
	tests := []struct{ requestID, want string }{
		{"abc/../../internal", "/videos/abc%2F..%2F..%2Finternal"},
		{".", "/videos/%252E"},
		{"..", "/videos/%252E%252E"},
	}
	for _, tt := range tests {
		t.Run(tt.requestID, func(t *testing.T) {
			var statusPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": tt.requestID})
					return
				}
				statusPath = r.URL.EscapedPath()
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "done",
					"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
				})
			}))
			defer server.Close()

			model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-imagine-video")
			if _, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"}); err != nil {
				t.Fatalf("DoGenerate: %v", err)
			}
			if statusPath != tt.want {
				t.Fatalf("status path = %q, want %q", statusPath, tt.want)
			}
		})
	}
}
