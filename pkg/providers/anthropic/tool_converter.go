package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// anthropicBuiltinToolTypes maps Go-AI SDK tool names to their Anthropic API type identifiers.
// Built-in Anthropic tools are sent to the API as {"type": "<type>"} without a schema.
var anthropicBuiltinToolTypes = map[string]string{
	"anthropic.computer_20251124":       "computer_20251124",
	"anthropic.bash_20250124":           "bash_20250124",
	"anthropic.text_editor_20250728":    "text_editor_20250728",
	"anthropic.code_execution_20250825": "code_execution_20250825",
	"anthropic.code_execution_20260120": "code-execution_20260120",
}

// ToAnthropicFormatWithCache converts tools to Anthropic's tool format with cache control support.
// This function wraps the standard ToAnthropicFormat and adds cache_control from ProviderOptions.
//
// If a tool has ProviderOptions of type *ToolOptions with CacheControl set, the cache_control
// field will be added to the tool definition to enable prompt caching.
//
// Example:
//
//	tools := []types.Tool{
//	    {
//	        Name:        "get_weather",
//	        Description: "Get weather information",
//	        Parameters:  schema,
//	        ProviderOptions: &anthropic.ToolOptions{
//	            CacheControl: &anthropic.CacheControl{
//	                Type: "ephemeral",
//	                TTL:  "5m",
//	            },
//	        },
//	    },
//	}
//	anthropicTools := anthropic.ToAnthropicFormatWithCache(tools)
// ToAnthropicFormatWithCache converts tools to Anthropic's tool format with cache control support.
// Built-in Anthropic tools (computer use, bash, text editor, code execution) are formatted
// as {"type": "<api-type>"} without a name or input_schema, as required by the Anthropic API.
// Regular function tools are formatted with name, description, and input_schema.
func ToAnthropicFormatWithCache(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))

	for i, t := range tools {
		// Check if this is a built-in Anthropic tool that needs special formatting
		if apiType, isBuiltin := anthropicBuiltinToolTypes[t.Name]; isBuiltin {
			toolMap := map[string]interface{}{
				"type": apiType,
			}
			// Built-in tools may still have cache_control
			if t.ProviderOptions != nil {
				if toolOpts, ok := t.ProviderOptions.(*ToolOptions); ok && toolOpts.CacheControl != nil {
					toolMap["cache_control"] = toolOpts.CacheControl
				}
			}
			result[i] = toolMap
			continue
		}

		// Regular function tool
		toolMap := tool.ToAnthropicFormat([]types.Tool{t})[0]

		// Add cache_control from ProviderOptions if present
		if t.ProviderOptions != nil {
			if toolOpts, ok := t.ProviderOptions.(*ToolOptions); ok && toolOpts.CacheControl != nil {
				toolMap["cache_control"] = toolOpts.CacheControl
			}
		}

		result[i] = toolMap
	}

	return result
}
