package codemode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TypeScript's code-mode/src/tool-prompt.test.ts ("code mode prompt").
//
// TypeScript's ToolSet is a plain object, whose keys iterate in
// declaration order; this port's ToolSet is a Go map, which has no
// defined iteration order, so tool catalogs render in sorted-name order
// (see sortedToolNames). Every case below uses tool names that already
// sort into the same order as the TypeScript source's declaration order,
// so the expected strings match the TS test's byte-for-byte.

const baseRules = "Execute code-mode TypeScript in an isolated sandbox.\n" +
	"\n" +
	"Put the full program in `js`; top-level `await`/`return` work. Return a JSON-serializable result.\n" +
	"Call host tools only as async `tools.name(input)`; await each or use `Promise.all` for independent calls.\n" +
	"Use exact names/types below. `JSON.parse`/`JSON.stringify` are available."

const disabledFetchPrefix = baseRules + "\n" +
	"Fetch: `fetch` is not available.\n" +
	"\n" +
	"Tools:"

func noopExecute(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
	return map[string]interface{}{}, nil
}

func TestBuildCodeModeToolDescription_SignaturesAndExamples(t *testing.T) {
	tools := ToolSet{
		"search": {
			Name:        "search",
			Description: "Search indexed documents.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"query", "mode"},
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query"},
					"limit": map[string]interface{}{"type": "integer"},
					"mode":  map[string]interface{}{"type": "string", "enum": []interface{}{"web", "files"}},
				},
			},
			OutputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"results"},
				"properties": map[string]interface{}{
					"results": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"id", "title", "score"},
							"properties": map[string]interface{}{
								"id":    map[string]interface{}{"type": "string"},
								"title": map[string]interface{}{"type": "string"},
								"score": map[string]interface{}{"type": "number"},
							},
						},
					},
				},
			},
			InputExamples: []types.ToolInputExample{{
				Input: map[string]interface{}{"query": "incident response notes", "limit": 5, "mode": "files"},
			}},
			Execute: noopExecute,
		},
		"summarize": {
			Name:        "summarize",
			Description: "Summarize text.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"text"},
				"properties": map[string]interface{}{
					"text":    map[string]interface{}{"type": "string"},
					"bullets": map[string]interface{}{"type": "boolean"},
				},
			},
			OutputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"summary"},
				"properties": map[string]interface{}{
					"summary": map[string]interface{}{"type": "string"},
					"bullets": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
			},
			Execute: noopExecute,
		},
	}

	got := BuildCodeModeToolDescription(tools, ToolDiscoveryDescription)
	want := disabledFetchPrefix + "\n" +
		"```ts\n" +
		"declare const tools: {\n" +
		"  /** Search indexed documents. */\n" +
		"  search: (input: {\n" +
		"    limit?: number;\n" +
		"    mode: \"web\" | \"files\";\n" +
		"    /** Search query */\n" +
		"    query: string;\n" +
		"  }) => Promise<{ results: { id: string; score: number; title: string; }[]; }>;\n" +
		"  /** Summarize text. */\n" +
		"  summarize: (input: { bullets?: boolean; text: string; }) => Promise<{ bullets?: string[]; summary: string; }>;\n" +
		"};\n" +
		"```\n" +
		"\n" +
		"Tool call examples:\n" +
		"```ts\n" +
		"const [search, summarize] = await Promise.all([\n" +
		"  tools.search({\"limit\":5,\"mode\":\"files\",\"query\":\"incident response notes\"}),\n" +
		"  tools.summarize({\"bullets\":true,\"text\":\"string\"}),\n" +
		"]);\n" +
		"return {\n" +
		"  search: { results: search.results },\n" +
		"  summarize: { bullets: summarize.bullets },\n" +
		"};\n" +
		"```"
	if got != want {
		t.Fatalf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	assertWordCountAtMost(t, got, 160)
}

func TestBuildCodeModeToolDescription_BracketAccessForNonIdentifierNames(t *testing.T) {
	tools := ToolSet{
		"web-search": {
			Name: "web-search",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"q"},
				"properties": map[string]interface{}{
					"q": map[string]interface{}{"type": "string"},
				},
			},
			OutputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"urls"},
				"properties": map[string]interface{}{
					"urls": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
			},
			Execute: noopExecute,
		},
	}

	got := BuildCodeModeToolDescription(tools, ToolDiscoveryDescription)
	want := disabledFetchPrefix + "\n" +
		"```ts\n" +
		"declare const tools: {\n" +
		"  \"web-search\": (input: { q: string; }) => Promise<{ urls: string[]; }>;\n" +
		"};\n" +
		"```\n" +
		"\n" +
		"Tool call examples:\n" +
		"```ts\n" +
		"const result = await tools[\"web-search\"]({\"q\":\"string\"});\n" +
		"return { urls: result.urls };\n" +
		"```"
	if got != want {
		t.Fatalf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	assertWordCountAtMost(t, got, 90)
}

func TestBuildCodeModeToolDescription_NestedJSONSchemaShapes(t *testing.T) {
	tools := ToolSet{
		"report": {
			Name: "report",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"items", "metadata"},
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"id"},
							"properties": map[string]interface{}{
								"id":    map[string]interface{}{"type": "string"},
								"score": map[string]interface{}{"type": "number"},
							},
						},
					},
					"metadata": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
					},
				},
			},
			OutputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"accepted", "ids"},
				"properties": map[string]interface{}{
					"accepted": map[string]interface{}{"type": "boolean"},
					"ids":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
			},
			Execute: noopExecute,
		},
	}

	got := BuildCodeModeToolDescription(tools, ToolDiscoveryDescription)
	want := disabledFetchPrefix + "\n" +
		"```ts\n" +
		"declare const tools: {\n" +
		"  report: (input: { items: { id: string; score?: number; }[]; metadata: Record<string, string>; }) => Promise<{ accepted: boolean; ids: string[]; }>;\n" +
		"};\n" +
		"```\n" +
		"\n" +
		"Tool call examples:\n" +
		"```ts\n" +
		"const result = await tools.report({\"items\":[{\"id\":\"string\",\"score\":1}],\"metadata\":{}});\n" +
		"return { accepted: result.accepted };\n" +
		"```"
	if got != want {
		t.Fatalf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	assertWordCountAtMost(t, got, 100)
}

func TestBuildCodeModeToolDescription_NoHostTools(t *testing.T) {
	got := BuildCodeModeToolDescription(ToolSet{}, ToolDiscoveryDescription)
	want := disabledFetchPrefix + "\n" + "No host tools. Do not call `tools.*`."
	if got != want {
		t.Fatalf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	assertWordCountAtMost(t, got, 65)
}

func TestCodeModeTool_ConversationDiscoveryDescription(t *testing.T) {
	tool := CodeModeTool(ToolCallerOptions{ToolDiscovery: ToolDiscoveryConversation})
	want := "Execute code-mode TypeScript in an isolated sandbox.\n" +
		"\n" +
		"Put the full program in `js`; top-level `await`/`return` work. Return a JSON-serializable result.\n" +
		"Call host tools only as async `tools.name(input)`; await each or use `Promise.all` for independent calls.\n" +
		"Use exact names/types from the latest capability update. `JSON.parse`/`JSON.stringify` are available.\n" +
		"Fetch: `fetch` is not available.\n" +
		"\n" +
		"Tools:\n" +
		"The current host-tool API is provided in \"Code mode capability update\" user messages. Follow the latest catalog and ignore earlier catalogs."
	if tool.Description != want {
		t.Fatalf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", tool.Description, want)
	}
	if tool.ExperimentalToolCaller == nil || tool.ExperimentalToolCaller.PrepareModelMessage == nil {
		t.Fatal("expected PrepareModelMessage to be set for conversation discovery")
	}
}

func assertWordCountAtMost(t *testing.T, description string, max int) {
	t.Helper()
	words := strings.Fields(strings.TrimSpace(description))
	if len(words) > max {
		t.Fatalf("word count %d exceeds max %d", len(words), max)
	}
}
