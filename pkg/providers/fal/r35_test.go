package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS reads video width/height/seed as z.number(): fractional values are valid
// and must not fail decoding the status body.
func TestR35_VideoStatusAcceptsFractionalNumbers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"video":{"url":"https://fal.media/v.mp4","width":1280.5,"height":720,"duration":5},"seed":1.5e3,"has_nsfw_concepts":[false]}`))
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil {
		t.Fatalf("DoStatus: %v", err)
	}
	meta := status.ProviderMetadata["fal"].(map[string]interface{})
	b, _ := json.Marshal(meta)
	var got struct {
		Seed   float64
		Videos []struct{ Width, Height float64 }
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Seed != 1500 || got.Videos[0].Width != 1280.5 || got.Videos[0].Height != 720 {
		t.Errorf("metadata = %s", b)
	}
	if meta["seed"] != 1500 {
		t.Errorf("whole seed should stay int, got %T", meta["seed"])
	}
}
