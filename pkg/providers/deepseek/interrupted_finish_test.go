package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23814: a clean end without the provider's completion signal reports
// interrupted and preserves the partial output; an explicit finish_reason and
// a parse error keep their own reasons.
func TestDeepseekStream_InterruptedWithoutFinishReason(t *testing.T) {
	chunks := drainDeepseek(t, `data: {"choices":[{"index":0,"delta":{"content":"hi"}}]}`+"\n\n"+"data: [DONE]\n\n")
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finish = %+v", last)
	}
	if last.RawFinishReason != "" {
		t.Fatalf("raw finish reason = %q", last.RawFinishReason)
	}
}
