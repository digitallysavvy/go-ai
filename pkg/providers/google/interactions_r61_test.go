package google

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r61: TS compaction matches an assistant message when any of its parts carries
// providerOptions.google.interactionId, tool-result and reasoning-file parts
// included.
func TestR61InteractionsCompactionMatchesEveryAssistantPartType(t *testing.T) {
	opts := map[string]interface{}{"google": map[string]interface{}{"interactionId": "v1_prev"}}
	for name, part := range map[string]types.ContentPart{
		"tool-result":    types.ToolResultContent{ToolCallID: "c1", ToolName: "google_search", ProviderExecuted: true, ProviderOptions: opts},
		"reasoning-file": types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1}, ProviderOptions: opts},
	} {
		msgs := []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{part}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
		}
		out := compactMessagesForInteraction(msgs, "v1_prev")
		if len(out) != 2 || out[0].Role != types.RoleUser || out[1].Role != types.RoleUser {
			t.Fatalf("%s: compacted messages = %#v, want only the two user messages", name, out)
		}
	}
}

// r61: TS formats static video offsets with a template literal
// (`${offset}s`), i.e. JavaScript's Number-to-string: exponent notation for
// tiny and huge values, and "0" for negative zero.
func TestR61InteractionsVideoOffsetUsesJSNumberString(t *testing.T) {
	for in, want := range map[float64]string{1e-7: "1e-7s", 1e21: "1e+21s", math.Copysign(0, -1): "0s", 10.5: "10.5s", 3: "3s"} {
		if got := videoOffsetString(in); got != want {
			t.Fatalf("videoOffsetString(%v) = %v, want %q", in, got, want)
		}
	}
}
