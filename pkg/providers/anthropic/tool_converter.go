package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

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

	// code execution — Anthropic API requires type only, no name field
	"anthropic.code_execution_20250522": {apiType: "code_execution_20250522"},
	"anthropic.code_execution_20250825": {apiType: "code_execution_20250825"},
	"anthropic.code_execution_20260120": {apiType: "code_execution_20260120"},

	// memory
	"anthropic.memory_20250818": {apiType: "memory_20250818", name: "memory"},
}

// anthropicAPIMapper is satisfied by ProviderOptions types that produce their own
// Anthropic API tool map. Used by tools that require per-instance config fields:
// computer tools (display dims), text_editor_20250728 (max_characters), web tools (filters).
type anthropicAPIMapper interface {
	ToAnthropicAPIMap() map[string]interface{}
}

// ToAnthropicFormatWithCache converts tools to Anthropic's tool format with full
// provider option support:
//
//   - Simple built-in tools (bash, text_editor w/o params, code_execution, memory):
//     formatted as {"type": "<api-type>", "name": "<api-name>"} with optional cache_control.
//
//   - Self-serializing provider tools (computer tools, text_editor_20250728, web_search,
//     web_fetch): formatted via ToAnthropicAPIMap() on their ProviderOptions, which includes
//     all required per-instance fields (display dims, max_characters, domain filters, etc.).
//
//   - Custom function tools: formatted with name, description, input_schema, and
//     optional cache_control / eager_input_streaming / defer_loading / allowed_callers.
func ToAnthropicFormatWithCache(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))

	for i, t := range tools {
		// 1. Simple builtin tools — {"type": ..., "name": ..., cache_control?}
		if def, isBuiltin := anthropicBuiltinToolTypes[t.Name]; isBuiltin {
			toolMap := map[string]interface{}{
				"type": def.apiType,
			}
			if def.name != "" {
				toolMap["name"] = def.name
			}
			if t.ProviderOptions != nil {
				if toolOpts, ok := t.ProviderOptions.(*ToolOptions); ok && toolOpts.CacheControl != nil {
					toolMap["cache_control"] = toolOpts.CacheControl
				}
			}
			result[i] = toolMap
			continue
		}

		// 2. Self-serializing provider tools (computer, text_editor_20250728, web_search, web_fetch).
		// These implement ToAnthropicAPIMap() on their ProviderOptions to produce a complete map
		// including all required per-instance fields.
		if t.ProviderOptions != nil {
			if mapper, ok := t.ProviderOptions.(anthropicAPIMapper); ok {
				result[i] = mapper.ToAnthropicAPIMap()
				continue
			}
		}

		// 3. Regular custom function tool.
		toolMap := tool.ToAnthropicFormat([]types.Tool{t})[0]

		// Apply ToolOptions: cache_control, eager_input_streaming, defer_loading, allowed_callers.
		if t.ProviderOptions != nil {
			if toolOpts, ok := t.ProviderOptions.(*ToolOptions); ok {
				if toolOpts.CacheControl != nil {
					toolMap["cache_control"] = toolOpts.CacheControl
				}
				if toolOpts.EagerInputStreaming != nil && *toolOpts.EagerInputStreaming {
					toolMap["eager_input_streaming"] = true
				}
				if toolOpts.DeferLoading != nil {
					toolMap["defer_loading"] = *toolOpts.DeferLoading
				}
				if len(toolOpts.AllowedCallers) > 0 {
					toolMap["allowed_callers"] = toolOpts.AllowedCallers
				}
			}
		}

		// Serialize InputExamples if present (requires advanced-tool-use beta, injected separately).
		if len(t.InputExamples) > 0 {
			examples := make([]interface{}, len(t.InputExamples))
			for j, ex := range t.InputExamples {
				examples[j] = ex.Input
			}
			toolMap["input_examples"] = examples
		}

		result[i] = toolMap
	}

	return result
}
