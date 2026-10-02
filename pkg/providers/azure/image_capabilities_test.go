package azure

import "testing"

// Ports the "image editing capabilities" matrix from
// azure-openai-provider.test.ts (TS #19230): Azure deployment names are
// user-defined, so file/mask input support is always unknown.
func TestImageModelSupportsFileAndMaskInputsAlwaysUnknown(t *testing.T) {
	for _, deploymentName := range []string{"gpt-image-production", "dall-e-3"} {
		t.Run(deploymentName, func(t *testing.T) {
			m := NewImageModel(&Provider{}, deploymentName)
			if v := m.SupportsFileInputs(); v != nil {
				t.Errorf("SupportsFileInputs() = %v, want nil (unknown)", *v)
			}
			if v := m.SupportsMaskInputs(); v != nil {
				t.Errorf("SupportsMaskInputs() = %v, want nil (unknown)", *v)
			}
		})
	}
}
