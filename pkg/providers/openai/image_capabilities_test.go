package openai

import "testing"

// Ports the editing-capability coverage implied by
// openai-image-model.test.ts (TS #19230): known editing-capable models
// report both capabilities true, dall-e-3 explicitly reports false, and
// unrecognized model IDs report unknown (nil).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    *bool
	}{
		{"dall-e-2", "dall-e-2", boolPtr(true)},
		{"gpt-image-1", "gpt-image-1", boolPtr(true)},
		{"gpt-image-1-mini", "gpt-image-1-mini", boolPtr(true)},
		{"chatgpt-image-latest", "chatgpt-image-latest", boolPtr(true)},
		{"dall-e-3", "dall-e-3", boolPtr(false)},
		{"unrecognized model", "gpt-image-unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
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
