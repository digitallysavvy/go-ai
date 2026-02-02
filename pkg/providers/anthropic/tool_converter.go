package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

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
func ToAnthropicFormatWithCache(tools []types.Tool) []map[string]interface{} {
	// Start with base Anthropic format
	result := tool.ToAnthropicFormat(tools)

	// Add cache_control from ProviderOptions if present
	for i, t := range tools {
		if t.ProviderOptions != nil {
			// Try to assert as *ToolOptions
			if toolOpts, ok := t.ProviderOptions.(*ToolOptions); ok {
				if toolOpts.CacheControl != nil {
					result[i]["cache_control"] = toolOpts.CacheControl
				}
			}
		}
	}

	return result
}
