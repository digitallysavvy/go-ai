package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r56: TS sets finishReason {unified: 'error', raw: undefined} on an
// exception, an unparsable chunk and a schema failure, so a raw stop reason
// from an earlier messageStop does not survive into the finish part.
func TestR56BedrockStreamErrorClearsRawFinishReason(t *testing.T) {
	for name, ev := range map[string][2]string{
		"exception":  {"throttlingException", `{"message":"slow down"}`},
		"unparsable": {"contentBlockDelta", `{`},
		"invalid":    {"contentBlockDelta", `5`},
	} {
		t.Run(name, func(t *testing.T) {
			chunks, err := r55CollectBedrockStream(t, [][2]string{
				{"messageStop", `{"stopReason":"end_turn"}`},
				ev,
			})
			if err != nil {
				t.Fatalf("stream error = %v", err)
			}
			var finish *provider.StreamChunk
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeFinish {
					finish = c
				}
			}
			if finish == nil {
				t.Fatal("no finish part")
			}
			if finish.FinishReason != types.FinishReasonError || finish.RawFinishReason != "" {
				t.Fatalf("finish = %q raw %q, want error with no raw reason", finish.FinishReason, finish.RawFinishReason)
			}
		})
	}
}
