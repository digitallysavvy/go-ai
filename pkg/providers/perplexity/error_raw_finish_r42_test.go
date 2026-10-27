package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS sets finishReason to { unified: 'error', raw: undefined } on a bad chunk,
// dropping the raw reason a completed event supplied earlier.
func TestPerplexityAgentStreamBadChunkClearsRawFinishReason(t *testing.T) {
	body := `data: {"type":"response.completed","response":{"id":"r","created_at":1,"model":"m","object":"response","output":[],"status":"completed"}}` + "\n\n" +
		`data: {"type":5}` + "\n\n"
	finish := perplexityAgentFinish(t, body)
	if finish.FinishReason != types.FinishReasonError {
		t.Fatalf("finish reason = %q, want error", finish.FinishReason)
	}
	if finish.RawFinishReason != "" {
		t.Fatalf("raw finish reason = %q, want empty", finish.RawFinishReason)
	}
}
