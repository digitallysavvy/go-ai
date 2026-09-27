package anthropic

// builtinToolDef holds the API type and canonical name for a simple Anthropic builtin tool.
// Simple builtins are those that require no per-instance config fields beyond name and type.
type builtinToolDef struct {
	apiType string // value of "type" field in the API request
	name    string // value of "name" field in the API request
}

// anthropicBuiltinToolTypes maps Go-AI SDK tool names to their Anthropic API serialization.
// Simple builtin tools that need no per-instance config are listed here.
// Tools that require extra fields (computer display dims, text_editor max_characters, web tool
// config, etc.) implement anthropicAPIMapper on their ProviderOptions instead.
var anthropicBuiltinToolTypes = map[string]builtinToolDef{
	// bash
	"anthropic.bash_20241022": {apiType: "bash_20241022", name: "bash"},
	"anthropic.bash_20250124": {apiType: "bash_20250124", name: "bash"},

	// text editors (no per-instance config)
	"anthropic.text_editor_20241022": {apiType: "text_editor_20241022", name: "str_replace_editor"},
	"anthropic.text_editor_20250124": {apiType: "text_editor_20250124", name: "str_replace_editor"},
	"anthropic.text_editor_20250429": {apiType: "text_editor_20250429", name: "str_replace_based_edit_tool"},

	// code execution (TS prepareTools sends name "code_execution")
	"anthropic.code_execution_20250522": {apiType: "code_execution_20250522", name: "code_execution"},
	"anthropic.code_execution_20250825": {apiType: "code_execution_20250825", name: "code_execution"},
	"anthropic.code_execution_20260120": {apiType: "code_execution_20260120", name: "code_execution"},

	// memory
	"anthropic.memory_20250818": {apiType: "memory_20250818", name: "memory"},

	// advisor
	"anthropic.advisor_20260301": {apiType: "advisor_20260301", name: "advisor"},

	// tool search
	"anthropic.tool_search_regex_20251119": {apiType: "tool_search_tool_regex_20251119", name: "tool_search_tool_regex"},
	"anthropic.tool_search_bm25_20251119":  {apiType: "tool_search_tool_bm25_20251119", name: "tool_search_tool_bm25"},
}

// BuiltinToolAPIName returns the short Anthropic API "name" for a simple
// built-in provider tool identified by its Go SDK tool Name (e.g.
// "anthropic.tool_search_bm25_20251119" -> "tool_search_tool_bm25"). Exported
// so other packages that forward Anthropic provider-defined tools through a
// different wire API (e.g. pkg/providers/bedrock's Converse toolConfig, which
// has no separate "type" field and instead needs a plain
// {name, inputSchema}) can reuse the same name table instead of keeping a
// separate copy — mirrors how TS bedrock imports `anthropicTools` from
// '@ai-sdk/anthropic/internal' to do a generic tool-id lookup. Only covers
// the simple builtins in anthropicBuiltinToolTypes (bash, text editors,
// code execution, memory, advisor, tool search); self-serializing tools
// (computer, text_editor_20250728, web_search, web_fetch) require
// per-instance config this helper does not have access to and are not
// included.
func BuiltinToolAPIName(name string) (string, bool) {
	def, ok := anthropicBuiltinToolTypes[name]
	if !ok || def.name == "" {
		return "", false
	}
	return def.name, true
}

// anthropicAPIMapper is satisfied by ProviderOptions types that produce their own
// Anthropic API tool map. Used by tools that require per-instance config fields:
// computer tools (display dims), text_editor_20250728 (max_characters), web tools (filters).
type anthropicAPIMapper interface {
	ToAnthropicAPIMap() map[string]interface{}
}
