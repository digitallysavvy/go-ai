package google

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS ef2dfb5e9f google-video-model.test.ts "should pass seed when it is 0".
func TestR87provaVideoSeedZeroIsSent(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ":predictLongRunning") {
			_ = json.NewDecoder(r.Body).Decode(&captured)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})
	seed := 0
	if _, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", N: 1, Seed: &seed},
	}); err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params, _ := captured["parameters"].(map[string]interface{})
	if v, ok := params["seed"]; !ok || v != float64(0) {
		t.Fatalf("parameters = %#v, want seed 0", params)
	}
}
