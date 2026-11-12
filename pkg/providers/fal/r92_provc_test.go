package fal

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcImageBodyReservedKeys (r92 provc-09): the TS requestBody
// literal {prompt, seed, image_size, num_images} fixes those positions, so a
// passthrough option that sets image_size (fal's named sizes) is written
// third, not after the other options.
func TestR92provcImageBodyReservedKeys(t *testing.T) {
	m := &ImageModel{modelID: "fal-ai/flux/dev"}
	n := 2
	body, _, err := m.buildRequest(&provider.ImageGenerateOptions{
		Prompt: "p", N: &n,
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{"guidanceScale": 3, "image_size": "square_hd"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body)
	if want := `{"prompt":"p","image_size":"square_hd","num_images":2,"guidance_scale":3}`; string(got) != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

// TestR92provcVideoDurationIsJSText (r92 provc-10): TS `${duration}s` writes
// the JS number text, and `if (options.duration)` skips 0 and NaN.
func TestR92provcVideoDurationIsJSText(t *testing.T) {
	m := &VideoModel{modelID: "fal-ai/luma-dream-machine"}
	for d, want := range map[float64]string{5: `"5s"`, 2.5: `"2.5s"`, 1e21: `"1e+21s"`, 1e-7: `"1e-7s"`} {
		dur := d
		body, err := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &dur})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := body.Get("duration")
		if got, _ := json.Marshal(v); string(got) != want {
			t.Errorf("duration %v = %s, want %s", d, got, want)
		}
	}
	nan := math.NaN()
	body, _ := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan})
	if _, ok := body.Get("duration"); ok {
		t.Error("a NaN duration is falsy in TS and must be skipped")
	}
}
