package bfl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS doGenerate throws AISDKError({name:
// 'BLACK_FOREST_LABS_VIDEO_GENERATION_FAILED', message: statusResult.error}):
// the error text is the status error alone. Go wrapped it as "video
// generation failed [provider=bfl, model=...]: <msg>" (provc r89 open item).
func TestR89integBFLVideoFailureText(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/" + VideoModelFlux3Video:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Error"})
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), VideoModelFlux3Video)
	model.provider.config.PollIntervalMillis = 1
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "x"})
	want := `Black Forest Labs video generation failed with status "Error". Request id: req-1`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var vge *providererrors.VideoGenerationError
	if !errors.As(err, &vge) || vge.Provider != "bfl" {
		t.Fatalf("err = %T, want *VideoGenerationError (v0.5.2 type kept)", err)
	}
}
