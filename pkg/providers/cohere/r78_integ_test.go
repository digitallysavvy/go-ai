package cohere

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
func TestR78integcohereReadsPastDone(t *testing.T) {
	body := `data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text","text":""}}}}` + "\n\n" +
		`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"Hello"}}}}` + "\n\n" +
		"data: [DONE]\n\n" +
		`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":" world"}}}}` + "\n\n" +
		`data: {"type":"content-end","index":0,"delta":{}}` + "\n\n" +
		`data: {"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":1,"output_tokens":2}}}}` + "\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
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
