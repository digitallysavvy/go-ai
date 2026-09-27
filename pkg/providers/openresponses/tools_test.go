package openresponses

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestToolsCustomTool verifies that Tools.CustomTool builds a "provider"
// tool whose ProviderArgs (description/format) match the shape
// convertToolsToOpenResponses expects, mirroring TS's
// createOpenResponsesTools({customToolId}).customTool.
func TestToolsCustomTool(t *testing.T) {
	toolsFactory := NewTools("acme.custom")

	tool := toolsFactory.CustomTool("render", CustomToolOptions{
		Description: "Render SVG markup.",
		Format:      &CustomToolFormat{Type: "grammar", Syntax: "lark", Definition: "start: OBJECT"},
	})

	if tool.Name != "render" {
		t.Fatalf("Name = %q, want render", tool.Name)
	}
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "acme.custom" {
		t.Fatalf("Type/ProviderID = %q/%q", tool.Type, tool.ProviderID)
	}
	if tool.ProviderExecuted {
		t.Fatalf("ProviderExecuted = true, want false (caller-executed, TS isProviderExecuted: false)")
	}
	if tool.ProviderArgs["description"] != "Render SVG markup." {
		t.Fatalf("ProviderArgs[description] = %v", tool.ProviderArgs["description"])
	}
	format, ok := tool.ProviderArgs["format"].(map[string]interface{})
	if !ok || format["type"] != "grammar" || format["syntax"] != "lark" || format["definition"] != "start: OBJECT" {
		t.Fatalf("ProviderArgs[format] = %+v", tool.ProviderArgs["format"])
	}

	// The factory-built tool round-trips through convertToolsToOpenResponses
	// exactly like the hand-built types.Tool literal other tests use.
	converted, _, warnings := convertToolsToOpenResponses([]types.Tool{tool}, nil, "acme.custom")
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
	custom, ok := converted[0].(CustomToolItem)
	if !ok || custom.Type != "custom" || custom.Name != "render" {
		t.Fatalf("converted[0] = %#v", converted[0])
	}
}

// TestToolsCustomTool_TextFormat verifies the unconstrained "text" format
// (no syntax/definition), and that Execute is carried through for caller
// execution (custom tools are caller-executed, not provider-executed).
func TestToolsCustomTool_TextFormat(t *testing.T) {
	toolsFactory := NewTools("acme.custom")
	executed := false
	tool := toolsFactory.CustomTool("write_svg", CustomToolOptions{
		Format: &CustomToolFormat{Type: "text"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return input["input"], nil
		},
	})

	format, ok := tool.ProviderArgs["format"].(map[string]interface{})
	if !ok || format["type"] != "text" {
		t.Fatalf("ProviderArgs[format] = %+v, want {type: text}", tool.ProviderArgs["format"])
	}
	if _, hasSyntax := format["syntax"]; hasSyntax {
		t.Fatalf("format = %+v, want no syntax key for text format", format)
	}
	if tool.Execute == nil {
		t.Fatal("Execute = nil, want the supplied function")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"input": "hi"}, types.ToolExecutionOptions{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !executed {
		t.Fatal("Execute was not invoked")
	}
}

// TestProviderTools verifies Provider.Tools() defaults its scope to
// "open-responses.custom" when Config.CustomToolID is unset (mirroring TS's
// `customToolId ?? 'open-responses.custom'` -- always on, never nil), and
// otherwise scopes to the configured id.
func TestProviderTools(t *testing.T) {
	p := New(Config{BaseURL: "https://example.com", Name: "acme"})
	tool := p.Tools().CustomTool("render", CustomToolOptions{})
	if tool.ProviderID != "open-responses.custom" {
		t.Fatalf("ProviderID = %q, want the open-responses.custom default", tool.ProviderID)
	}

	p2 := New(Config{BaseURL: "https://example.com", Name: "acme", CustomToolID: "acme.custom"})
	tool2 := p2.Tools().CustomTool("render", CustomToolOptions{})
	if tool2.ProviderID != "acme.custom" {
		t.Fatalf("ProviderID = %q, want acme.custom", tool2.ProviderID)
	}
}
