package tool

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestIsExecutableTool(t *testing.T) {
	executable := &types.Tool{
		Name: "weather",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
	if !IsExecutableTool(executable) {
		t.Fatal("IsExecutableTool() = false, want true")
	}
	if IsExecutableTool(&types.Tool{Name: "schema-only"}) {
		t.Fatal("schema-only tool should not be executable")
	}
	if IsExecutableTool(nil) {
		t.Fatal("nil tool should not be executable")
	}
}

func TestRequireExecutableToolAllowsExecutionAfterNarrowing(t *testing.T) {
	toolDef := &types.Tool{
		Name: "weather",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{
				"city":      input["city"],
				"requestId": options.ToolContext.(map[string]interface{})["requestId"],
			}, nil
		},
	}
	executable, ok := RequireExecutableTool(toolDef)
	if !ok {
		t.Fatal("RequireExecutableTool() ok = false, want true")
	}
	result, err := executable.Execute(context.Background(), map[string]interface{}{"city": "Berlin"}, types.ToolExecutionOptions{
		ToolContext: map[string]interface{}{"requestId": "req-1"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := result.(map[string]interface{})
	if got["city"] != "Berlin" || got["requestId"] != "req-1" {
		t.Fatalf("Execute() result = %#v", got)
	}
}
