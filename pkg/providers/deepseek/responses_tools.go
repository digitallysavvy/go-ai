package deepseek

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// WebSearchToolID is the provider tool ID of DeepSeek's server-side web
// search (TS deepseek.tools.webSearch, id "deepseek.web_search").
const WebSearchToolID = "deepseek.web_search"

// WebSearch is DeepSeek's server-side web search for the Responses API
// (TS deepseek.tools.webSearch). DeepSeek runs the searches and page reads
// during generation and adds the results to the model context; it is sent
// as {"type": "web_search"}. Supported with deepseek-v4-pro.
//
// Successful searches and page reads are billed per operation and reported
// in Usage.Raw["server_tool_use"] and providerMetadata.deepseek.serverToolUse.
func WebSearch() types.Tool {
	return types.Tool{
		Name:             "web_search",
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       WebSearchToolID,
		ProviderArgs:     map[string]interface{}{},
		ProviderExecuted: true,
		Description:      "DeepSeek server-side web search.",
		Parameters:       map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		// TS webSearchOutputSchema: {status?: string, action?: record}.
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{"type": "string"},
				"action": map[string]interface{}{"type": "object"},
			},
		},
	}
}

// DeepSeekTools are the DeepSeek server-side tools (TS deepseekTools).
type DeepSeekTools struct{}

// WebSearch is DeepSeek's server-side web search (TS
// deepseek.tools.webSearch()).
func (DeepSeekTools) WebSearch() types.Tool { return WebSearch() }

// Tools returns the DeepSeek server-side tools for the Responses API (TS
// provider.tools).
func (p *Provider) Tools() DeepSeekTools { return DeepSeekTools{} }

// prepareDeepSeekResponsesTools is TS prepareDeepSeekResponsesTools: the
// web search provider tool becomes {type: web_search}, other provider tools
// are dropped with a warning, function tools are sent without strict (a
// strict setting warns), and the tool choice maps to auto / none / required
// or {type: function, name}.
func prepareDeepSeekResponsesTools(tools []types.Tool, toolChoice types.ToolChoice) ([]interface{}, interface{}, []types.Warning) {
	var warnings []types.Warning
	if len(tools) == 0 {
		return nil, nil, warnings
	}
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		if t.Type == types.ToolTypeProviderDefined {
			if t.ProviderID == WebSearchToolID {
				out = append(out, map[string]interface{}{"type": "web_search"})
			} else {
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("provider-defined tool %s", firstNonEmptyDS(t.ProviderID, t.Name))})
			}
			continue
		}
		if t.Strict != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict tool calls",
				Details: "The DeepSeek Responses API does not support strict mode.",
			})
		}
		def := map[string]interface{}{"type": "function", "name": t.Name}
		if t.Description != "" {
			def["description"] = t.Description
		}
		fn, _ := tool.ToJSONSchema(t)["function"].(map[string]interface{})
		def["parameters"] = fn["parameters"]
		out = append(out, def)
	}
	if toolChoice.Type == "" {
		return out, nil, warnings
	}
	switch toolChoice.Type {
	case types.ToolChoiceAuto, types.ToolChoiceNone, types.ToolChoiceRequired:
		return out, string(toolChoice.Type), warnings
	case types.ToolChoiceTool:
		for _, t := range tools {
			if t.Name == toolChoice.ToolName && t.Type == types.ToolTypeProviderDefined {
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("toolChoice for provider-defined tool %s", t.ProviderID)})
				return out, nil, warnings
			}
		}
		return out, map[string]interface{}{"type": "function", "name": toolChoice.ToolName}, warnings
	}
	return out, nil, warnings
}
