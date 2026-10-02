package googlevertex

import "testing"

// Ports the editing-capability coverage implied by
// google-vertex-image-model.test.ts (TS #19230): known Gemini image models
// report file support true and mask support false; unrecognized gemini-*
// IDs report unknown (nil).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    *bool
	}{
		{"gemini-2.5-flash-image", "gemini-2.5-flash-image", boolPtr(true)},
		{"gemini-3-pro-image-preview", "gemini-3-pro-image-preview", boolPtr(true)},
		{"gemini-3.1-flash-image-preview", "gemini-3.1-flash-image-preview", boolPtr(true)},
		{"unrecognized gemini model", "gemini-9-unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
			gotFile := m.SupportsFileInputs()
			if !boolPtrEqual(gotFile, tt.want) {
				t.Errorf("SupportsFileInputs() = %v, want %v", gotFile, tt.want)
			}
			var wantMask *bool
			if tt.want != nil {
				wantMask = boolPtr(false)
			}
			gotMask := m.SupportsMaskInputs()
			if !boolPtrEqual(gotMask, wantMask) {
				t.Errorf("SupportsMaskInputs() = %v, want %v", gotMask, wantMask)
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
