package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func openResponsesFinish(t *testing.T, body string) *provider.StreamChunk {
	t.Helper()
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil, "open-responses")
	var finish *provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk")
	}
	return finish
}

// TS #23814: a stream that ends without a terminal response event reports
// interrupted; a malformed event reports error.
func TestStreamInterruptedWithoutTerminalEvent(t *testing.T) {
	body := `data: {"type":"response.output_item.added","item":{"type":"message","id":"m1","role":"assistant","content":[]}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","item_id":"m1","delta":"hi"}` + "\n\n"
	if got := openResponsesFinish(t, body).FinishReason; got != types.FinishReasonInterrupted {
		t.Fatalf("finish reason = %q", got)
	}
}

func TestStreamParseErrorFinishesWithError(t *testing.T) {
	body := `data: {"type":"response.output_text.delta","item_id":"m1","delta":"hi"}` + "\n\n" + "data: not-json\n\n"
	if got := openResponsesFinish(t, body).FinishReason; got != types.FinishReasonError {
		t.Fatalf("finish reason = %q", got)
	}
}
