package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// zod `.optional()` enum: a present reasoningEffortUpdate must be valid; "",
// a number and an explicit null are rejected rather than ignored.
func TestR39_ReasoningEffortUpdateInvalidValuesRejected(t *testing.T) {
	for name, v := range map[string]interface{}{"empty": "", "number": 5, "null": nil} {
		t.Run(name, func(t *testing.T) {
			prompt := types.Prompt{Messages: []types.Message{{
				Role:            types.RoleSystem,
				ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffortUpdate": v}},
			}}}
			if _, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{ProviderOptionsName: "openai"}); err == nil {
				t.Fatal("expected an InvalidArgumentError")
			}
		})
	}
}
