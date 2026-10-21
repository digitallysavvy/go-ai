package fal

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doStatus reads a failed status body with response.json(); a non-JSON
// body throws a SyntaxError (not an APICallError), so doStatus rejects rather
// than reporting an error status.
func TestR36_VideoStatusNonJSONFailureBodyRejects(t *testing.T) {
	for name, body := range map[string]string{"html": "<html>502</html>", "empty": "", "null": "null"} {
		mux := http.NewServeMux()
		mux.HandleFunc("/fal-ai/m", videoQueueHandler(t, "id", "/fal-ai/m/requests/id", nil, nil))
		mux.HandleFunc("/fal-ai/m/requests/id", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(body))
		})
		model, _ := newTestVideoModel(t, "m", mux)
		start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
		if err == nil {
			t.Errorf("%s: DoStatus = %+v, want an error", name, status)
		}
	}
}
