package tool

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestToolSearchServerMode verifies that ToolSearch in server mode produces a
// tool with the correct name, ProviderExecuted flag, and serializable options.
func TestToolSearchServerMode(t *testing.T) {
	tool := ToolSearch(ToolSearchArgs{})

	if tool.Name != "openai.tool_search" {
		t.Errorf("Name = %q, want %q", tool.Name, "openai.tool_search")
	}
	if !tool.ProviderExecuted {
		t.Error("ProviderExecuted = false, want true for server mode")
	}

	opts, ok := tool.ProviderOptions.(ToolSearchOptions)
	if !ok {
		t.Fatalf("ProviderOptions type = %T, want ToolSearchOptions", tool.ProviderOptions)
	}
	if opts.Execution != "server" {
		t.Errorf("Execution = %q, want %q", opts.Execution, "server")
	}
}

// TestToolSearchServerMode_ExplicitExecution verifies explicit "server" value.
func TestToolSearchServerMode_ExplicitExecution(t *testing.T) {
	tool := ToolSearch(ToolSearchArgs{Execution: "server"})

	if !tool.ProviderExecuted {
		t.Error("ProviderExecuted = false, want true for explicit server mode")
	}
	opts := tool.ProviderOptions.(ToolSearchOptions)
	if opts.Execution != "server" {
		t.Errorf("Execution = %q, want %q", opts.Execution, "server")
	}
}

// TestToolSearchClientMode verifies that ToolSearch in client mode produces a
// tool with ProviderExecuted=false and the Execute function is wired correctly.
func TestToolSearchClientMode(t *testing.T) {
	called := false
	var capturedInput map[string]interface{}

	tool := ToolSearch(ToolSearchArgs{
		Execution:   "client",
		Description: "Find tools matching a query",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			capturedInput = input
			return []string{"get_weather"}, nil
		},
	})

	if tool.Name != "openai.tool_search" {
		t.Errorf("Name = %q, want %q", tool.Name, "openai.tool_search")
	}
	if tool.ProviderExecuted {
		t.Error("ProviderExecuted = true, want false for client mode")
	}
	if tool.Description != "Find tools matching a query" {
		t.Errorf("Description = %q", tool.Description)
	}

	opts, ok := tool.ProviderOptions.(ToolSearchOptions)
	if !ok {
		t.Fatalf("ProviderOptions type = %T, want ToolSearchOptions", tool.ProviderOptions)
	}
	if opts.Execution != "client" {
		t.Errorf("Execution = %q, want %q", opts.Execution, "client")
	}

	// Invoke Execute to verify call routing
	result, err := tool.Execute(context.Background(), map[string]interface{}{"query": "weather"}, types.ToolExecutionOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Error("Execute function was not called")
	}
	if capturedInput["query"] != "weather" {
		t.Errorf("captured input query = %v, want %q", capturedInput["query"], "weather")
	}

	names, ok := result.([]string)
	if !ok || len(names) != 1 || names[0] != "get_weather" {
		t.Errorf("Execute result = %v, want [get_weather]", result)
	}
}

// TestToolSearchClientMode_CallIDRoundtrip verifies that call IDs passed in
// ToolExecutionOptions are accessible to the Execute function.
func TestToolSearchClientMode_CallIDRoundtrip(t *testing.T) {
	var capturedOpts types.ToolExecutionOptions

	tool := ToolSearch(ToolSearchArgs{
		Execution: "client",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			capturedOpts = opts
			return nil, nil
		},
	})

	execOpts := types.ToolExecutionOptions{ToolCallID: "call_search_001"}
	_, err := tool.Execute(context.Background(), map[string]interface{}{}, execOpts)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if capturedOpts.ToolCallID != "call_search_001" {
		t.Errorf("ToolCallID = %q, want %q", capturedOpts.ToolCallID, "call_search_001")
	}
}

// TestToolSearchClientMode_NoExecute verifies that client mode without Execute
// returns an error rather than panicking.
func TestToolSearchClientMode_NoExecute(t *testing.T) {
	tool := ToolSearch(ToolSearchArgs{Execution: "client"})

	_, err := tool.Execute(context.Background(), map[string]interface{}{}, types.ToolExecutionOptions{})
	if err == nil {
		t.Error("expected error when Execute is nil, got nil")
	}
}
