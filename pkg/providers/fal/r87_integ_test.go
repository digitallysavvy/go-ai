package fal

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS fal-video-model.test.ts "should pass seed when it is 0" (ef2dfb5e9f:
// seed != null).
func TestR87integVideoSeedZeroIsSent(t *testing.T) {
	var captured map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", &captured, nil))
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)
	seed := 0
	if _, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", Seed: &seed},
	}); err != nil {
		t.Fatal(err)
	}
	if v, ok := captured["seed"]; !ok || v != float64(0) {
		t.Errorf("seed = %v (present %v), want 0", v, ok)
	}
}
