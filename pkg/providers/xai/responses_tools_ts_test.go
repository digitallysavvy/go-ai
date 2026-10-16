package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareResponsesTools: an unknown provider-defined tool is dropped with an
// `unsupported` warning "provider-defined tool <name>".
func TestXAIResponsesUnknownProviderToolWarnsAndIsDropped(t *testing.T) {
	tools := []types.Tool{
		{Name: "mystery", Type: "provider", ProviderID: "xai.mystery"},
		{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
	}
	prepared, warnings := prepareXAIResponsesToolsWithWarnings(tools)
	if len(prepared) != 1 || prepared[0].(map[string]interface{})["type"] != "function" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "provider-defined tool mystery" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

// TS fileSearchArgsSchema: vectorStoreIds is required (empty array is valid).
func TestXAIResponsesFileSearchRequiresVectorStoreIDs(t *testing.T) {
	if err := validateXAIResponsesToolArgs([]types.Tool{FileSearch(FileSearchConfig{})}); err == nil {
		t.Fatal("expected a validation error without vectorStoreIds")
	}
	if err := validateXAIResponsesToolArgs([]types.Tool{FileSearch(FileSearchConfig{VectorStoreIDs: []string{}})}); err != nil {
		t.Fatalf("empty array must be valid: %v", err)
	}
	ts := types.Tool{Name: "fs", Type: "provider", ProviderID: "xai.file_search", ProviderArgs: map[string]interface{}{}}
	if err := validateXAIResponsesToolArgs([]types.Tool{ts}); err == nil {
		t.Fatal("expected a validation error for a TS-shaped tool without vectorStoreIds")
	}
	ts.ProviderArgs = map[string]interface{}{"vectorStoreIds": []interface{}{"vs_1"}, "maxNumResults": 3.0}
	if err := validateXAIResponsesToolArgs([]types.Tool{ts}); err != nil {
		t.Fatal(err)
	}
	m := prepareXAIResponsesTools([]types.Tool{ts})[0].(map[string]interface{})
	if m["type"] != "file_search" || m["max_num_results"] != 3 {
		t.Fatalf("wire = %#v", m)
	}
}

// A TS-shaped web search tool serializes from its camelCase ProviderArgs.
func TestXAIResponsesTSShapedToolUsesProviderArgs(t *testing.T) {
	tool := types.Tool{Name: "search", Type: "provider", ProviderID: "xai.web_search",
		ProviderArgs: map[string]interface{}{"allowedDomains": []interface{}{"a.com"}}}
	m := prepareXAIResponsesTools([]types.Tool{tool})[0].(map[string]interface{})
	if m["type"] != "web_search" {
		t.Fatalf("wire = %#v", m)
	}
	if got, _ := m["allowed_domains"].([]string); len(got) != 1 || got[0] != "a.com" {
		t.Fatalf("wire = %#v", m)
	}
}

// TS: toolChoice naming a provider tool is omitted with a warning; an unknown
// tool name is omitted silently; a function tool is forced.
func TestXAIResponsesToolChoiceResolution(t *testing.T) {
	tools := []types.Tool{WebSearch(WebSearchConfig{}), {Name: "lookup", Parameters: map[string]interface{}{"type": "object"}}}
	got, warnings := resolveXAIResponsesToolChoice(types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "xai.web_search"}, tools)
	if got != nil || len(warnings) != 1 || warnings[0].Feature != `toolChoice for server-side tool "xai.web_search"` {
		t.Fatalf("got %v %v", got, warnings)
	}
	if got, w := resolveXAIResponsesToolChoice(types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "nope"}, tools); got != nil || len(w) != 0 {
		t.Fatalf("got %v %v", got, w)
	}
	got, _ = resolveXAIResponsesToolChoice(types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "lookup"}, tools)
	if m, _ := got.(map[string]interface{}); m["name"] != "lookup" {
		t.Fatalf("got %v", got)
	}
}

// TS fileSearchArgsSchema / mcpServerArgsSchema validate arg types, not only
// presence: wrong-typed args fail before the request is built.
func TestXAIResponsesFileSearchAndMCPArgTypeValidation(t *testing.T) {
	fs := func(args map[string]interface{}) error {
		return validateXAIResponsesToolArgs([]types.Tool{{Name: "fs", Type: "provider", ProviderID: "xai.file_search", ProviderArgs: args}})
	}
	mcp := func(args map[string]interface{}) error {
		return validateXAIResponsesToolArgs([]types.Tool{{Name: "m", Type: "provider", ProviderID: "xai.mcp", ProviderArgs: args}})
	}
	bad := []struct {
		name string
		err  error
	}{
		{"fs non-string store id", fs(map[string]interface{}{"vectorStoreIds": []interface{}{"vs", 3.0}})},
		{"fs store ids not array", fs(map[string]interface{}{"vectorStoreIds": "vs"})},
		{"fs maxNumResults string", fs(map[string]interface{}{"vectorStoreIds": []interface{}{}, "maxNumResults": "3"})},
		{"mcp missing serverUrl", mcp(map[string]interface{}{})},
		{"mcp serverUrl number", mcp(map[string]interface{}{"serverUrl": 1.0})},
		{"mcp allowedTools non-string", mcp(map[string]interface{}{"serverUrl": "u", "allowedTools": []interface{}{1.0}})},
		{"mcp headers non-string", mcp(map[string]interface{}{"serverUrl": "u", "headers": map[string]interface{}{"a": 1.0}})},
		{"mcp authorization bool", mcp(map[string]interface{}{"serverUrl": "u", "authorization": true})},
	}
	for _, b := range bad {
		if b.err == nil {
			t.Errorf("%s: expected a validation error", b.name)
		}
	}
	if err := mcp(map[string]interface{}{"serverUrl": "u", "serverLabel": "l", "allowedTools": []interface{}{"a"}, "headers": map[string]interface{}{"h": "v"}, "authorization": "t"}); err != nil {
		t.Fatalf("valid mcp args rejected: %v", err)
	}
	if err := validateXAIResponsesToolArgs([]types.Tool{MCPServer(MCPServerConfig{ServerURL: "https://x"})}); err != nil {
		t.Fatalf("typed mcp rejected: %v", err)
	}
}
