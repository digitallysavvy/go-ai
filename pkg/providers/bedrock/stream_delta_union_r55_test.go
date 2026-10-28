package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS parses contentBlockDelta.delta with a zod union whose first matching
// member wins and strips the other keys, so an empty text (or a citation, or
// an empty reasoning text) shadows a toolUse or later reasoning field.
func TestR55BedrockStreamDeltaUnionFirstMemberWins(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delta string
	}{
		{"empty text shadows toolUse", `{"text":"","toolUse":{"input":"{}"}}`},
		{"citation shadows toolUse", `{"citation":{"x":1},"toolUse":{"input":"{}"}}`},
		{"empty text shadows reasoning", `{"text":"","reasoningContent":{"text":"r"}}`},
		{"empty reasoning text shadows signature", `{"reasoningContent":{"text":"","signature":"sig"}}`},
		{"empty signature shadows data", `{"reasoningContent":{"signature":"","data":"d"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := r55CollectBedrockStream(t, [][2]string{
				{"contentBlockStart", `{"contentBlockIndex":0,"start":{"toolUse":{"toolUseId":"t1","name":"f"}}}`},
				{"contentBlockDelta", `{"contentBlockIndex":0,"delta":` + tc.delta + `}`},
				{"messageStop", `{"stopReason":"end_turn"}`},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range chunks {
				switch c.Type {
				case provider.ChunkTypeToolInputDelta, provider.ChunkTypeReasoning, provider.ChunkTypeReasoningStart, provider.ChunkTypeText, provider.ChunkTypeError:
					t.Fatalf("unexpected %s chunk: %#v", c.Type, c)
				}
			}
		})
	}
}
