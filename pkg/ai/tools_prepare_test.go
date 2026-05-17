package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestResolveStepTools_DescriptionFunc(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{{
		Name:        "weather",
		Description: "static",
		DescriptionFunc: func(_ context.Context, options types.ToolDescriptionOptions) string {
			if options.Context.(string) != "ctx" {
				t.Fatalf("context = %#v, want ctx", options.Context)
			}
			if options.ExperimentalSandbox.(string) != "sbx" {
				t.Fatalf("sandbox = %#v, want sbx", options.ExperimentalSandbox)
			}
			return "dynamic"
		},
	}}

	got := resolveStepTools(context.Background(), tools, map[string]interface{}{"weather": "ctx"}, "sbx")
	if got[0].Description != "dynamic" {
		t.Fatalf("description = %q, want dynamic", got[0].Description)
	}
}

func TestEnrichToolCallMetadata_FromToolDefinition(t *testing.T) {
	t.Parallel()
	calls := []types.ToolCall{{ID: "1", ToolName: "weather"}}
	tools := []types.Tool{{Name: "weather", Metadata: map[string]interface{}{"source": "mcp"}}}
	got := enrichToolCallMetadata(calls, tools)
	if got[0].ToolMetadata["source"] != "mcp" {
		t.Fatalf("tool metadata = %#v, want source=mcp", got[0].ToolMetadata)
	}
}

func TestEnrichToolCallMetadata_ClonesToolMetadata(t *testing.T) {
	t.Parallel()
	calls := []types.ToolCall{{ID: "1", ToolName: "weather"}}
	tools := []types.Tool{{Name: "weather", Metadata: map[string]interface{}{"source": "mcp"}}}

	got := enrichToolCallMetadata(calls, tools)
	got[0].ToolMetadata["source"] = "changed"

	if tools[0].Metadata["source"] != "mcp" {
		t.Fatalf("tool metadata mutated = %#v, want original map untouched", tools[0].Metadata)
	}
}
