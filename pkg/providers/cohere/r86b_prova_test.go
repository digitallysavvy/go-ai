package cohere

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS parseJSON({text: pendingToolCall.arguments?.trim() || '{}'}): the JS trim
// strips U+FEFF / U+3000 around the arguments (Go's TrimSpace kept U+FEFF and
// the call failed with a JSONParseError).
func TestR86bprovaStreamToolArgumentsJSTrim(t *testing.T) {
	sse := "data: {\"type\":\"tool-call-start\",\"index\":0,\"delta\":{\"message\":{\"tool_calls\":{\"id\":\"tc1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"\\ufeff{\\\"b\\\": 1, \\\"a\\\": 2}\\u3000\"}}}}}\n\n" +
		"data: {\"type\":\"tool-call-end\",\"index\":0,\"delta\":{}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(sse)))
	for i := 0; i < 10; i++ {
		c, err := s.Next()
		if err != nil {
			t.Fatalf("stream error = %v", err)
		}
		if c.Type == provider.ChunkTypeToolCall {
			if c.ToolCall == nil || c.ToolCall.RawArguments != `{"b":1,"a":2}` {
				t.Fatalf("tool call = %#v", c.ToolCall)
			}
			return
		}
	}
	t.Fatal("no tool-call chunk")
}
