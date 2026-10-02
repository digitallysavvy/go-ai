package bytedance

import "testing"

// Ports the "capabilities" matrix from bytedance-image-model.test.ts
// (TS #19230): file-input-capable Seedream models report masks unsupported.
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		wantFileInputs *bool
		wantMaskInputs *bool
	}{
		{"seedream-4-0", "seedream-4-0-250828", boolPtr(true), boolPtr(false)},
		{"seedream-4-5", "seedream-4-5-251128", boolPtr(true), boolPtr(false)},
		{"seedream-5-0", "seedream-5-0-260128", boolPtr(true), boolPtr(false)},
		{"seedream-5-0-lite", "seedream-5-0-lite-260128", boolPtr(true), boolPtr(false)},
		{"dola-seedream-5-0-pro", "dola-seedream-5-0-pro-260628", boolPtr(true), boolPtr(false)},
		{"unknown model", "seedream-unknown", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newImageModel(&Provider{}, tt.modelID)
			gotFile := m.SupportsFileInputs()
			gotMask := m.SupportsMaskInputs()
			if !boolPtrEqual(gotFile, tt.wantFileInputs) {
				t.Errorf("SupportsFileInputs() = %v, want %v", gotFile, tt.wantFileInputs)
			}
			if !boolPtrEqual(gotMask, tt.wantMaskInputs) {
				t.Errorf("SupportsMaskInputs() = %v, want %v", gotMask, tt.wantMaskInputs)
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
