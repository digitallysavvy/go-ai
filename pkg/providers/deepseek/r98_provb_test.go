package deepseek

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r98provbDrain reads a stream to its end and returns the parts and the
// final error (nil at a clean end).
func r98provbDrain(s provider.TextStream) ([]*provider.StreamChunk, error) {
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, c)
	}
}

// TestR98provbChatErrorKeyOnDataChunk: TS deepseekChatChunkSchema tries the
// data member first, and its z.object strips an `error` key, so a frame with
// `choices` and an `error` member is a data chunk (`'error' in value` is
// false). Go emitted an error part and an error finish.
func TestR98provbChatErrorKeyOnDataChunk(t *testing.T) {
	for _, errValue := range []string{`null`, `{"message":"x"}`} {
		sse := `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":` + errValue + "}\n\n"
		parts, err := r98provbDrain(newDeepseekStream(io.NopCloser(strings.NewReader(sse))))
		if err != nil {
			t.Fatal(err)
		}
		text, finish := "", ""
		for _, p := range parts {
			switch p.Type {
			case provider.ChunkTypeText:
				text += p.Text
			case provider.ChunkTypeFinish:
				finish = string(p.FinishReason)
			case provider.ChunkTypeError:
				t.Errorf("error %s: unexpected error part %v", errValue, p.Err)
			}
		}
		if text != "hi" || finish != "stop" {
			t.Errorf("error %s: text %q finish %q, want hi / stop", errValue, text, finish)
		}
	}
	// A frame without choices fails the data member: TS error member.
	parts, _ := r98provbDrain(newDeepseekStream(io.NopCloser(strings.NewReader("data: {\"error\":{\"message\":\"boom\"}}\n\n"))))
	if len(parts) == 0 || parts[0].Type != provider.ChunkTypeError || parts[0].Text != "boom" {
		t.Errorf("error frame parts = %+v, want the boom error part first", parts)
	}
}
