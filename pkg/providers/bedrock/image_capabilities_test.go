package bedrock

import "testing"

// Ports the "capabilities" matrix from amazon-bedrock-image-model.test.ts
// (TS #19230): advertises file/mask input support for Nova Canvas only.
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		wantFileInputs *bool
		wantMaskInputs *bool
	}{
		{"nova canvas", "amazon.nova-canvas-v1:0", boolPtr(true), boolPtr(true)},
		{"unknown model", "custom-image-model", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
			gotFile := m.SupportsFileInputs()
			gotMask := m.SupportsMaskInputs()
			if (gotFile == nil) != (tt.wantFileInputs == nil) || (gotFile != nil && *gotFile != *tt.wantFileInputs) {
				t.Errorf("SupportsFileInputs() = %v, want %v", derefBool(gotFile), derefBool(tt.wantFileInputs))
			}
			if (gotMask == nil) != (tt.wantMaskInputs == nil) || (gotMask != nil && *gotMask != *tt.wantMaskInputs) {
				t.Errorf("SupportsMaskInputs() = %v, want %v", derefBool(gotMask), derefBool(tt.wantMaskInputs))
			}
		})
	}
}

func derefBool(b *bool) interface{} {
	if b == nil {
		return nil
	}
	return *b
}
