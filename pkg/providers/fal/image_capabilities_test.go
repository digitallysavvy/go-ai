package fal

import "testing"

// Ports the editing-capability coverage implied by fal-image-model.test.ts
// (TS #19230): image-to-image models report file support true, inpainting
// models additionally report mask support true, text-to-image-only models
// report false, and unrecognized model IDs report unknown (nil).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		wantFileInputs *bool
		wantMaskInputs *bool
	}{
		{"flux-general image-to-image", "fal-ai/flux-general/image-to-image", boolPtr(true), boolPtr(false)},
		{"flux-general inpainting", "fal-ai/flux-general/inpainting", boolPtr(true), boolPtr(true)},
		{"flux-lora inpainting", "fal-ai/flux-lora/inpainting", boolPtr(true), boolPtr(true)},
		{"flux-pro kontext", "fal-ai/flux-pro/kontext", boolPtr(true), boolPtr(false)},
		{"bria text-to-image", "fal-ai/bria/text-to-image/base", boolPtr(false), boolPtr(false)},
		{"recraft text-to-image", "fal-ai/recraft/v3/text-to-image", boolPtr(false), boolPtr(false)},
		{"unknown model", "fal-ai/unknown-model", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
			if got := m.SupportsFileInputs(); !boolPtrEqual(got, tt.wantFileInputs) {
				t.Errorf("SupportsFileInputs() = %v, want %v", got, tt.wantFileInputs)
			}
			if got := m.SupportsMaskInputs(); !boolPtrEqual(got, tt.wantMaskInputs) {
				t.Errorf("SupportsMaskInputs() = %v, want %v", got, tt.wantMaskInputs)
			}
		})
	}
}

func boolPtrEqual(a, b *bool) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}
