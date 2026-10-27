package cohere

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS keeps the last finishReason written: an invalid chunk after message-end
// turns the finish into "error", while the message-end usage is kept.
func TestCohereStream_InvalidChunkAfterMessageEndFinishesWithError(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":3,"output_tokens":4}}}}`,
		`data: {"type":"content-end"}`,
	}, "\n\n") + "\n\n"
	chunks := drainCohere(t, body)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonError {
		t.Fatalf("finish = %+v, want error", last)
	}
	if last.Usage == nil || last.Usage.InputTokens == nil || *last.Usage.InputTokens != 3 {
		t.Fatalf("usage = %+v, want message-end usage kept", last.Usage)
	}
}
