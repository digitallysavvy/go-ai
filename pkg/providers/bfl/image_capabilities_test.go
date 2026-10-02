package bfl

import "testing"

// Ports the "capabilities" matrix from black-forest-labs-image-model.test.ts
// (TS #19230).
func TestImageModelSupportsFileAndMaskInputs(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		wantFileInputs *bool
		wantMaskInputs *bool
	}{
		{"flux-pro-1.0-fill", "flux-pro-1.0-fill", boolPtr(true), boolPtr(true)},
		{"flux-kontext-pro", "flux-kontext-pro", boolPtr(true), boolPtr(false)},
		{"flux-kontext-max", "flux-kontext-max", boolPtr(true), boolPtr(false)},
		{"flux-pro-1.1-ultra", "flux-pro-1.1-ultra", boolPtr(false), boolPtr(false)},
		{"flux-pro-1.1", "flux-pro-1.1", boolPtr(false), boolPtr(false)},
		{"unknown model", "flux-unknown", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewImageModel(&Provider{}, tt.modelID)
			gotFile := m.SupportsFileInputs()
			gotMask := m.SupportsMaskInputs()
			assertBoolPtrEqual(t, "SupportsFileInputs", gotFile, tt.wantFileInputs)
			assertBoolPtrEqual(t, "SupportsMaskInputs", gotMask, tt.wantMaskInputs)
		})
	}
}

func assertBoolPtrEqual(t *testing.T, label string, got, want *bool) {
	t.Helper()
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Errorf("%s() = %v, want %v", label, boolPtrString(got), boolPtrString(want))
	}
}

func boolPtrString(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}
