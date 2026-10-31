package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A custom (text) tool call carries TS's `JSON.stringify(text)` as
// RawArguments and the text as Arguments {"input": text}; it must not be
// rejected as a non-object tool input.
func TestParseToolCallCustomTextInput(t *testing.T) {
	tools := []types.Tool{{Name: "fmt", Parameters: map[string]interface{}{"type": "object"}}}
	got, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "1", ToolName: "fmt", RawArguments: `"x"`, Arguments: map[string]interface{}{"input": "x"}},
		Tools:    tools,
	})
	if err != nil || got.Invalid || got.Arguments["input"] != "x" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// Without the decoded Arguments a bare string is still not an object.
	bad, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "2", ToolName: "fmt", RawArguments: `"x"`},
		Tools:    tools,
	})
	if err != nil || !bad.Invalid {
		t.Fatalf("bad=%+v err=%v", bad, err)
	}
}
