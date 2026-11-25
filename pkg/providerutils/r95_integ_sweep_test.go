package providerutils

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A `Choices` or `Usage` key is not the TS schema's `choices` / `usage`
// (zod matches object keys exactly; review round 95 repo sweep).
func TestR95integChunkChoicesAndWireUsageKeysMatchExactly(t *testing.T) {
	if err := RequireChunkChoices([]byte(`{"Choices":[]}`)); err == nil {
		t.Fatal("RequireChunkChoices accepted a Choices key")
	}
	if err := RequireChunkChoices([]byte(`{"choices":[]}`)); err != nil {
		t.Fatal(err)
	}
	result := &types.GenerateResult{}
	ApplyWireUsageRaw(result, []byte(`{"Usage":{"prompt_tokens":1}}`))
	if result.Usage.Raw != nil {
		t.Fatalf("Usage.Raw = %v, want none from a Usage key", result.Usage.Raw)
	}
	ApplyWireUsageRaw(result, []byte(`{"usage":{"prompt_tokens":1}}`))
	if result.Usage.Raw == nil {
		t.Fatal("usage key not applied")
	}
}
