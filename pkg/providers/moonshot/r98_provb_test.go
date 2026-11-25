package moonshot

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR98provbErrorKeyOnDataChunk: TS moonshotAIChatChunkSchema tries the
// data member first, and its z.object strips an `error` key, so a frame with
// `choices` and an error object is a data chunk (`'error' in value` is
// false). Go emitted an error part and an error finish.
func TestR98provbErrorKeyOnDataChunk(t *testing.T) {
	sse := `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":{"message":"x"}}` + "\n\n"
	s := newMoonshotStream(io.NopCloser(strings.NewReader(sse)), false)
	text, finish := "", ""
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeFinish:
			finish = string(c.FinishReason)
		case provider.ChunkTypeError:
			t.Errorf("unexpected error part %v", c.Err)
		}
	}
	if text != "hi" || finish != "stop" {
		t.Errorf("text %q finish %q, want hi / stop", text, finish)
	}
}
