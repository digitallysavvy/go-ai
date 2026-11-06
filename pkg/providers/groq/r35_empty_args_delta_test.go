package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS emits a tool-input-delta for a present-but-empty `arguments` string on an
// existing call (`arguments != null`) and nothing for a null one.
func TestGroqStream_EmptyArgumentsDeltaEmitted(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":""}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":""}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":null}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	n := 0
	for _, c := range drainGroq(t, sse) {
		if c.Type == provider.ChunkTypeToolInputDelta {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tool-input-delta count = %d, want 1 (present empty arguments only)", n)
	}
}
