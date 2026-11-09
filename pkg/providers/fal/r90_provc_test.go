package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provcVideoOperationKeepsWebhookSubmitURL: TS submitToQueue returns
// submitUrl = config.url({path: queuePath}) after the fal_webhook query is
// appended (fal-video-model.ts 211-222), and doStart puts it in the
// operation. Go stored the queue path without the query.
func TestR90provcVideoOperationKeepsWebhookSubmitURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"request_id":   "id",
			"response_url": "http://" + r.Host + "/fal-ai/luma-dream-machine/requests/id",
		})
	})
	model, srv := newTestVideoModel(t, "luma-dream-machine", mux)
	res, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
		WebhookURL:              "https://smee.io/abc123",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	var op falVideoOperation
	if err := json.Unmarshal(res.Operation, &op); err != nil {
		t.Fatalf("operation: %v", err)
	}
	want := srv.URL + "/fal-ai/luma-dream-machine?fal_webhook=https%3A%2F%2Fsmee.io%2Fabc123"
	if op.SubmitURL != want {
		t.Fatalf("submitUrl = %q, want %q", op.SubmitURL, want)
	}
	if !strings.HasSuffix(op.ResponseURL, "/requests/id") {
		t.Fatalf("responseUrl = %q", op.ResponseURL)
	}
}
