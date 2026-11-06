package gemini

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS takes the codeExecutionResult branch for any such part, so a result
// with no executable code before it emits nothing, not its text.
func TestR55StreamCodeExecutionResultWithoutCodeDoesNotFallThroughToText(t *testing.T) {
	body := `data: {"candidates":[{"content":{"parts":[{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"},"text":"leak"}]},"finishReason":"STOP"}]}` + "\n\n"
	s := newTestStream(io.NopCloser(strings.NewReader(body)))
	for _, c := range drainStream(t, s) {
		if c.Type == provider.ChunkTypeText || c.Type == provider.ChunkTypeTextStart || c.Type == provider.ChunkTypeToolResult {
			t.Fatalf("unexpected %s chunk", c.Type)
		}
	}
}

// Generate: TS pushes a tool-result for every codeExecutionResult part (with
// an undefined toolCallId before any code part) and skips its text.
func TestR55ConvertResponseCodeExecutionResultWithoutCodeIsToolResult(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"},"text":"leak"}]},"finishReason":"STOP"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want one tool result", result.Content)
	}
	if r, ok := result.Content[0].(types.ToolResultContent); !ok || r.ToolCallID != "" {
		t.Fatalf("content[0] = %#v, want a tool result with an empty id", result.Content[0])
	}
}
