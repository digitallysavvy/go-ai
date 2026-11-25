package perplexity

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading;
// frames after it count and the finish part comes once, at the end of the
// body (r78 provb change request, coordinator ruling a).
func TestR78integperplexityReadsPastDone(t *testing.T) {
	body := `data: {"type":"response.output_text.delta","item_id":"i1","output_index":0,"content_index":0,"delta":"Hello"}` + "\n\n" +
		"data: [DONE]\n\n" +
		`data: {"type":"response.output_text.delta","item_id":"i1","output_index":0,"content_index":0,"delta":" world"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	defer func() { _ = s.Close() }()
	var text strings.Builder
	var finishes []*provider.StreamChunk
	var last *provider.StreamChunk
	for i := 0; i < 1000; i++ {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("stream error: %v", err)
			}
			break
		}
		last = c
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		case provider.ChunkTypeError:
			t.Fatalf("unexpected error part: %+v", c)
		}
	}
	if text.String() != "Hello world" {
		t.Fatalf("text = %q, want %q", text.String(), "Hello world")
	}
	if len(finishes) != 1 || last != finishes[0] {
		t.Fatalf("want exactly one finish part, last; got %d, last %+v", len(finishes), last)
	}
	if finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %q", finishes[0].FinishReason)
	}
}
