package cohere

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23814: a clean end without message-end reports interrupted.
func TestCohereStream_InterruptedWithoutFinish(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text","text":""}}}}`,
		`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"hi"}}}}`,
	}, "\n\n") + "\n\n"
	chunks := drainCohere(t, body)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finish = %+v", last)
	}
}
