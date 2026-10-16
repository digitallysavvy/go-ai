package fal

import (
	"context"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"net/http"
	"reflect"
	"testing"
)

// zod strips unknown keys of the nested z.object schemas (timings, debug
// latents, the single `image`); only the top-level looseObject passes them.
func TestR33_NestedZodStripping(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "fal-fast")
	res, err := m.convertBody(context.Background(), []byte(`{
		"image":{"url":"data:image/png;base64,YQ==","width":1,"extra":true,"content_type":"image/png"},
		"timings":{"inference":1.5,"queue":3},
		"debug_latents":{"url":"u","junk":1},
		"top_extra":{"keep":{"nested":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	fal := res.ProviderMetadata["fal"].(map[string]interface{})
	if !reflect.DeepEqual(fal["timings"], map[string]interface{}{"inference": 1.5}) {
		t.Errorf("timings = %v", fal["timings"])
	}
	if !reflect.DeepEqual(fal["debug_latents"], map[string]interface{}{"url": "u"}) {
		t.Errorf("debug_latents = %v", fal["debug_latents"])
	}
	if !reflect.DeepEqual(fal["image"], map[string]interface{}{"url": "data:image/png;base64,YQ==", "width": 1.0, "content_type": "image/png"}) {
		t.Errorf("image = %v", fal["image"])
	}
	if _, ok := fal["top_extra"].(map[string]interface{})["keep"]; !ok {
		t.Errorf("top-level looseObject extra dropped: %v", fal)
	}
}

func TestR33_VideoMetadataNullishFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/m", videoQueueHandler(t, "id", "/fal-ai/m/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/m/requests/id", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"video":{"url":"https://fal.media/v.mp4","extra":1},"seed":null,"has_nsfw_concepts":null,"timings":{"inference":null,"queue":2}}`))
	})
	model, _ := newTestVideoModel(t, "m", mux)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil {
		t.Fatal(err)
	}
	meta := status.ProviderMetadata["fal"].(map[string]interface{})
	if v, ok := meta["seed"]; !ok || v != nil {
		t.Errorf("seed = %v (present %v)", v, ok)
	}
	if v, ok := meta["has_nsfw_concepts"]; !ok || v != nil {
		t.Errorf("has_nsfw_concepts = %v (present %v)", v, ok)
	}
	if !reflect.DeepEqual(meta["timings"], map[string]interface{}{"inference": nil}) {
		t.Errorf("timings = %#v", meta["timings"])
	}
	if meta["videos"].([]map[string]interface{})[0]["contentType"] != nil {
		t.Errorf("contentType = %#v", meta["videos"])
	}
}

// A non-array `detail` fails the validation branch but the {message} branch of
// the union still matches.
func TestR33_ImageErrorMessageUnionFallsBackToMessage(t *testing.T) {
	if got := falImageErrorMessage(400, []byte(`{"detail":"nope","message":"m"}`)); got != "m" {
		t.Fatalf("got %q", got)
	}
	if got := falImageErrorMessage(400, []byte(`{"detail":[],"message":"m"}`)); got != "" {
		t.Fatalf("empty valid detail list gives %q", got)
	}
	if got := falImageErrorMessage(400, []byte(`[1]`)); got != "Bad Request" {
		t.Fatalf("got %q", got)
	}
}
