package codemode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TypeScript's code-mode/src/tool-invocation.test.ts, "returns an AI
// SDK tool that executes code mode".
func TestCreateCodeModeTool_ExecutesCodeMode(t *testing.T) {
	tools := ToolSet{"add": {
		Name:       "add",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			a, _ := input["a"].(float64)
			b, _ := input["b"].(float64)
			return map[string]interface{}{"sum": a + b}, nil
		},
	}}
	codeMode := CreateCodeModeTool(tools, Options{})

	if codeMode.Name != DefaultToolName {
		t.Fatalf("got Name %q, want %q", codeMode.Name, DefaultToolName)
	}
	if codeMode.Execute == nil {
		t.Fatal("expected Execute to be set")
	}

	got, err := codeMode.Execute(
		context.Background(),
		map[string]interface{}{"js": "return await tools.add({ a: 4, b: 6 });"},
		types.ToolExecutionOptions{ToolCallID: "code"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"sum": float64(10)})
}

// Exercises CodeModeTool's ExperimentalToolCaller.Bind end to end: Bind is
// exactly what pkg/ai's experimental_toolCallers plumbing calls with the
// late-bound host tools (mirrors TypeScript's codeModeTool +
// experimental_toolCallers, see TypeScript's "late-binds host tools
// through generateText" -- the generic Bind/PrepareModelMessage plumbing
// itself is covered end to end in pkg/ai; this confirms code mode's Bind
// callback returns a tool that actually runs the bound tools).
func TestCodeModeTool_BindReturnsWorkingTool(t *testing.T) {
	codeMode := CodeModeTool(ToolCallerOptions{})
	if codeMode.ExperimentalToolCaller == nil || codeMode.ExperimentalToolCaller.Bind == nil {
		t.Fatal("expected ExperimentalToolCaller.Bind to be set")
	}

	boundTools := []types.Tool{{
		Name:       "add",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			a, _ := input["a"].(float64)
			b, _ := input["b"].(float64)
			return map[string]interface{}{"sum": a + b}, nil
		},
	}}
	bound := codeMode.ExperimentalToolCaller.Bind(boundTools)

	got, err := bound.Execute(
		context.Background(),
		map[string]interface{}{"js": "return await tools.add({ a: 4, b: 6 });"},
		types.ToolExecutionOptions{ToolCallID: "code"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"sum": float64(10)})
}

// Exercises CodeModeTool's ExperimentalToolCaller.PrepareModelMessage end
// to end for ToolDiscoveryConversation: the returned catalog message must
// name the bound tool. Mirrors part of TypeScript's "announces changed
// tools in conversation while keeping the model tool stable".
func TestCodeModeTool_PrepareModelMessage_AnnouncesBoundTools(t *testing.T) {
	codeMode := CodeModeTool(ToolCallerOptions{ToolDiscovery: ToolDiscoveryConversation})
	if codeMode.ExperimentalToolCaller == nil || codeMode.ExperimentalToolCaller.PrepareModelMessage == nil {
		t.Fatal("expected ExperimentalToolCaller.PrepareModelMessage to be set")
	}

	boundTools := []types.Tool{{
		Name:        "lookup",
		Description: "Look up a record.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"required":   []interface{}{"id"},
			"properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
		},
	}}
	msg := codeMode.ExperimentalToolCaller.PrepareModelMessage(boundTools)
	if msg == nil {
		t.Fatal("expected a non-nil catalog message")
	}
	if !strings.Contains(*msg, "Code mode capability update.") {
		t.Fatalf("expected catalog message to announce a capability update, got %q", *msg)
	}
	if !strings.Contains(*msg, "lookup: (input: { id: string; })") {
		t.Fatalf("expected catalog message to describe the lookup tool, got %q", *msg)
	}
}
