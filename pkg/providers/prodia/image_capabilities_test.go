package prodia

import "testing"

// Ports the implicit "always false" editing-capability behavior from
// prodia-image-model.ts (TS #19230): Prodia's image models are
// text-to-image only, so both capabilities are always false (never
// unknown).
func TestImageModelSupportsFileAndMaskInputsAlwaysFalse(t *testing.T) {
	m := NewImageModel(&Provider{}, "inference.flux.schnell.txt2img.v1")
	if v := m.SupportsFileInputs(); v == nil || *v != false {
		t.Errorf("SupportsFileInputs() = %v, want false", v)
	}
	if v := m.SupportsMaskInputs(); v == nil || *v != false {
		t.Errorf("SupportsMaskInputs() = %v, want false", v)
	}
}
