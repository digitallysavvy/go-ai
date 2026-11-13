package codex

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS fingerprints the turn configuration with JSON.stringify, which writes a
// non-finite number as null; encoding/json failed the whole value, so every
// configuration with one hashed as "null" (ruling 4, review round 97).
func TestR97toolsFingerprintNonFiniteSchemaNumber(t *testing.T) {
	withInf := fingerprintTurnConfiguration("x", []harness.ToolSpec{{Name: "t", InputSchema: map[string]any{"maximum": math.Inf(1)}}})
	withNull := fingerprintTurnConfiguration("x", []harness.ToolSpec{{Name: "t", InputSchema: map[string]any{"maximum": nil}}})
	other := fingerprintTurnConfiguration("y", []harness.ToolSpec{{Name: "t", InputSchema: map[string]any{"maximum": math.NaN()}}})
	if withInf != withNull {
		t.Fatalf("Inf fingerprint %s != null fingerprint %s", withInf, withNull)
	}
	if other == withInf {
		t.Fatal("different instructions hash alike")
	}
}
