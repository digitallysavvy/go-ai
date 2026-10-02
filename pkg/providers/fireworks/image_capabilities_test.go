package fireworks

import "testing"

// Ports the editing-capability coverage implied by
// fireworks-image-model.test.ts (TS #19230): Flux Kontext models report
// file support true; the remaining known text-to-image models report false;
// unrecognized model IDs report unknown (nil). Masks are always false when
// support is known and nil otherwise.
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		wantFileInputs *bool
	}{
		{"flux-kontext-pro", "accounts/fireworks/models/flux-kontext-pro", boolPtr(true)},
		{"flux-kontext-max", "accounts/fireworks/models/flux-kontext-max", boolPtr(true)},
		{"flux-1-dev-fp8", "accounts/fireworks/models/flux-1-dev-fp8", boolPtr(false)},
		{"stable-diffusion-xl", "accounts/fireworks/models/stable-diffusion-xl-1024-v1-0", boolPtr(false)},
		{"unknown model", "accounts/fireworks/models/unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
			gotFile := m.SupportsFileInputs()
			if !boolPtrEqual(gotFile, tt.wantFileInputs) {
				t.Errorf("SupportsFileInputs() = %v, want %v", gotFile, tt.wantFileInputs)
			}
			gotMask := m.SupportsMaskInputs()
			var wantMask *bool
			if tt.wantFileInputs != nil {
				wantMask = boolPtr(false)
			}
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
