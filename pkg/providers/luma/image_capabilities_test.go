package luma

import "testing"

// Ports the editing-capability coverage implied by luma-image-model.test.ts
// (TS #19230): photon models report file support true and mask support
// false; other model IDs report unknown (nil).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    *bool
	}{
		{"photon-1", "photon-1", boolPtr(true)},
		{"photon-flash-1", "photon-flash-1", boolPtr(true)},
		{"unrecognized model", "photon-unknown", nil},
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
