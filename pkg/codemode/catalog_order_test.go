package codemode

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestCodeModeTool_BindCatalogPreservesDeclarationOrder is a byte-exact
// regression test for the CM2 ordered-catalog fix.
//
// TypeScript's ToolSet is a plain object, whose keys -- and therefore the
// catalog buildCodeModeToolDescription renders via Object.entries(tools)
// (code-mode/src/tool-prompt.ts, verified against ai@7.0.118) -- iterate in
// declaration (insertion) order. Before this fix, ai.PrepareToolsForToolCallers
// threaded a local caller's routed tools through a Go map
// (map[string]types.Tool) on the way into ToolCallerDefinition.Bind, so
// CodeModeTool's generated catalog came out in whatever order Go's map
// happened to iterate -- unspecified, and in practice not the caller's
// declared order -- instead of matching TypeScript.
//
// The fixture below deliberately declares tools in non-alphabetical order
// ("zebra" before "apple") so a regression back to map iteration, or to
// alphabetical sorting (codemode's own narrower, documented fallback for
// the direct, non-tool-caller CreateCodeModeTool path -- see
// orderedFromToolSet), would flip the two entries and fail this test. The
// expected string is derived by hand from the same
// renderToolType/renderToolExamples algorithm already verified
// byte-for-byte against TS fixtures in tool_prompt_test.go.
func TestCodeModeTool_BindCatalogPreservesDeclarationOrder(t *testing.T) {
	stringInputSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"id"},
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"},
		},
	}
	zebra := types.Tool{
		Name:        "zebra",
		Description: "Zebra tool.",
		Parameters:  stringInputSchema,
		Execute:     noopExecute,
	}
	apple := types.Tool{
		Name:        "apple",
		Description: "Apple tool.",
		Parameters:  stringInputSchema,
		Execute:     noopExecute,
	}

	codeMode := CodeModeTool(ToolCallerOptions{})
	if codeMode.Name != DefaultToolName {
		t.Fatalf("codeMode.Name = %q, want %q", codeMode.Name, DefaultToolName)
	}

	// Declaration order: codeMode, zebra, apple -- matches a real
	// GenerateTextOptions.Tools slice, TS's nearest analogue of an
	// insertion-ordered tools object (see ToolCallerDefinition.Bind's doc).
	tools := []types.Tool{codeMode, zebra, apple}
	toolCallers := ai.ResolvedToolCallers{
		"zebra": {"codeMode"},
		"apple": {"codeMode"},
	}

	executionTools, _, err := ai.PrepareToolsForToolCallers(tools, toolCallers)
	if err != nil {
		t.Fatalf("PrepareToolsForToolCallers: %v", err)
	}

	var bound *types.Tool
	for i := range executionTools {
		if executionTools[i].Name == DefaultToolName {
			bound = &executionTools[i]
		}
	}
	if bound == nil {
		t.Fatalf("expected a bound %q tool in executionTools, got %v", DefaultToolName, toolNamesForTest(executionTools))
	}

	want := disabledFetchPrefix + "\n" +
		"```ts\n" +
		"declare const tools: {\n" +
		"  /** Zebra tool. */\n" +
		"  zebra: (input: { id: string; }) => Promise<unknown>;\n" +
		"  /** Apple tool. */\n" +
		"  apple: (input: { id: string; }) => Promise<unknown>;\n" +
		"};\n" +
		"```\n" +
		"\n" +
		"Tool call examples:\n" +
		"```ts\n" +
		"const [zebra, apple] = await Promise.all([\n" +
		"  tools.zebra({\"id\":\"string\"}),\n" +
		"  tools.apple({\"id\":\"string\"}),\n" +
		"]);\n" +
		"return {\n" +
		"  zebra: zebra,\n" +
		"  apple: apple,\n" +
		"};\n" +
		"```"

	if bound.Description != want {
		t.Fatalf("catalog order mismatch:\n--- got ---\n%s\n--- want ---\n%s", bound.Description, want)
	}
}

func toolNamesForTest(tools []types.Tool) []string {
	names := make([]string, len(tools))
	for i, tl := range tools {
		names[i] = tl.Name
	}
	return names
}
