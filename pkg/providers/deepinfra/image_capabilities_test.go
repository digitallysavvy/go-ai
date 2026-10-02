package deepinfra

import "testing"

// Ports the "supportsFileInputs"/"supportsMaskInputs" coverage implied by
// deepinfra-image-model.test.ts (TS #19230): known editing-capable models
// report both capabilities true; unknown models report nil (unknown).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    *bool
	}{
		{"flux-1-schnell", "black-forest-labs/FLUX-1-schnell", boolPtr(true)},
		{"qwen image edit", "Qwen/Qwen-Image-Edit", boolPtr(true)},
		{"sdxl-turbo", "stabilityai/sdxl-turbo", boolPtr(true)},
		{"unknown model", "unknown/model", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID, "", "")
			if got := m.SupportsFileInputs(); !boolPtrEqual(got, tt.want) {
				t.Errorf("SupportsFileInputs() = %v, want %v", got, tt.want)
			}
			if got := m.SupportsMaskInputs(); !boolPtrEqual(got, tt.want) {
				t.Errorf("SupportsMaskInputs() = %v, want %v", got, tt.want)
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
