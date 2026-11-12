package typesafeai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR91provcTypedOptionLiterals (typed-option ruling): a typed option map
// normalizes to the map the warning reader asserts.
func TestR91provcTypedOptionLiterals(t *testing.T) {
	m, ok := providerutils.NormalizeOptionsJSON(map[string]interface{}{"typesafe": map[string]bool{"longContext": true, "x": true}})["typesafe"].(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Fatalf("normalized = %#v", m)
	}
}
