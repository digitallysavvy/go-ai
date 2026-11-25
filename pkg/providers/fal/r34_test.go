package fal

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doStart and the "still in progress" doStatus both report
// `response.headers: undefined`; the queue response headers are not surfaced.
func TestR34_VideoStartAndPendingHaveNoResponseHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/m", videoQueueHandler(t, "id", "/fal-ai/m/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/m/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fal-Trace", "t")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"Request is still in progress"}`))
	})
	model, _ := newTestVideoModel(t, "m", mux)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if start.Response.Headers != nil {
		t.Errorf("doStart headers = %v, want nil", start.Response.Headers)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != provider.VideoOperationStatusPending || status.Response.Headers != nil {
		t.Errorf("pending status = %v headers = %v", status.Status, status.Response.Headers)
	}
}
