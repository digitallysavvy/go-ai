package typesafeai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR92provcReferenceRefineUsesJSTrim (r92 provc-18): TS refines the image
// reference with value.trim().length > 0, so a BOM-only reference is
// rejected and a NEL-only one is accepted (Go TrimSpace has the opposite
// sets).
func TestR92provcReferenceRefineUsesJSTrim(t *testing.T) {
	check := func(ref string) error {
		_, err := providerutils.ParseProviderOptions("typesafe", map[string]interface{}{"typesafe": map[string]interface{}{"reference": ref}}, typesafeFilePartSchema)
		return err
	}
	if err := check("\ufeff"); err == nil {
		t.Error("a BOM-only reference must be rejected")
	}
	if err := check("\u0085"); err != nil {
		t.Errorf("a NEL-only reference must be accepted: %v", err)
	}
}
