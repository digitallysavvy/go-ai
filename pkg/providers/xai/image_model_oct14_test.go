package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from TS #21980, #21982, #21983.
func TestXAIImageModel20AndCapabilities(t *testing.T) {
	if ModelGrokImagineImage20 != "grok-imagine-image-2.0" {
		t.Fatalf("ModelGrokImagineImage20 = %q", ModelGrokImagineImage20)
	}
	prov := New(Config{APIKey: "k"})
	for _, tc := range []struct {
		id        string
		supported bool
	}{
		{ModelGrokImagineImage, true},
		{ModelGrokImagineImage20, true},
		{ModelGrokImagineImagePro, true},
		{"grok-2-image", false},
	} {
		model := NewImageModel(prov, tc.id)
		file := provider.ImageModelSupportsFileInputs(model)
		mask := provider.ImageModelSupportsMaskInputs(model)
		if tc.supported {
			if file == nil || !*file || mask == nil || *mask {
				t.Errorf("%s: file=%v mask=%v, want true/false", tc.id, file, mask)
			}
		} else if file != nil || mask != nil {
			t.Errorf("%s: want unknown capabilities", tc.id)
		}
	}
}

func TestXAIImageResolutionAndQualityOptions(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	model := NewImageModel(prov, ModelGrokImagineImage)
	for _, resolution := range []string{"1k", "1.5k", "2k"} {
		opts, err := extractImageProviderOptions(map[string]interface{}{"xai": map[string]interface{}{"resolution": resolution}})
		if err != nil {
			t.Fatalf("resolution %s: %v", resolution, err)
		}
		body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "p"}, opts, false)
		if body["resolution"] != resolution {
			t.Errorf("resolution = %v, want %s", body["resolution"], resolution)
		}
	}
	for _, quality := range []string{"high", "auto"} {
		opts, err := extractImageProviderOptions(map[string]interface{}{"xai": map[string]interface{}{"quality": quality}})
		if err != nil {
			t.Fatalf("quality %s: %v", quality, err)
		}
		body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "p"}, opts, false)
		if body["quality"] != quality {
			t.Errorf("quality = %v, want %s", body["quality"], quality)
		}
	}
}

// TS #22225: xSearch accepts up to 20 allowed/excluded handles; Go imposes no
// lower cap, so all 20 reach the wire.
func TestXSearchAcceptsTwentyHandles(t *testing.T) {
	handles := make([]string, 20)
	for i := range handles {
		handles[i] = "handle"
	}
	prepared := prepareXAIResponsesTools([]types.Tool{XSearch(XSearchConfig{AllowedXHandles: handles, ExcludedXHandles: handles})})
	m, _ := prepared[0].(map[string]interface{})
	if got, _ := m["allowed_x_handles"].([]string); len(got) != 20 {
		t.Errorf("allowed_x_handles = %v", m["allowed_x_handles"])
	}
	if got, _ := m["excluded_x_handles"].([]string); len(got) != 20 {
		t.Errorf("excluded_x_handles = %v", m["excluded_x_handles"])
	}
}
