package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doStatus treats a status-less response as done only when video.url is
// set; a persisted file_output.public_url alone stays pending.
func TestVideoStatusWithoutStatusNeedsVideoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"video": map[string]interface{}{"file_output": map[string]interface{}{"file_id": "f", "filename": "a.mp4", "public_url": "https://x.test/a.mp4"}},
		})
	}))
	defer server.Close()

	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-imagine-video")
	res, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"requestId":"r1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("status = %q, want pending", res.Status)
	}
}
