package perplexity

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func perplexityAgentFinish(t *testing.T, body string) *provider.StreamChunk {
	t.Helper()
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	for {
		chunk, err := s.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if chunk.Type == provider.ChunkTypeFinish {
			return chunk
		}
	}
}

// TS #23814: no terminal event reports interrupted; a terminal event with no
// response details reports other.
func TestPerplexityAgentStreamInterruptedWithoutTerminalEvent(t *testing.T) {
	body := `data: {"type":"response.output_text.delta","item_id":"i1","output_index":0,"content_index":0,"delta":"hi"}` + "\n\n"
	if got := perplexityAgentFinish(t, body).FinishReason; got != types.FinishReasonInterrupted {
		t.Fatalf("finish reason = %q", got)
	}
}

func TestPerplexityAgentStreamTerminalEventWithoutResponseIsOther(t *testing.T) {
	body := `data: {"type":"response.completed"}` + "\n\n"
	if got := perplexityAgentFinish(t, body).FinishReason; got != types.FinishReasonOther {
		t.Fatalf("finish reason = %q", got)
	}
}
