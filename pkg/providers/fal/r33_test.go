package fal

import (
	"context"
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
