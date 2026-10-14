package groq

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// ToolIDBrowserSearch is the provider tool ID of the Groq browser search tool,
// mirroring TS groq.tools.browserSearch.
const ToolIDBrowserSearch = "groq.browser_search"

// BrowserSearchTool creates the Groq browser search provider tool. It
// provides interactive browser search that navigates websites and returns more
// detailed results than traditional web search. The tool is executed by Groq
// and takes no arguments.
//
// See https://console.groq.com/docs/browser-search.
func BrowserSearchTool() types.Tool {
	return types.Tool{
		Name:             "browser_search",
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       ToolIDBrowserSearch,
		ProviderExecuted: true,
		Parameters: map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": false,
		},
		OutputSchema: map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": false,
		},
	}
}

// modelsWithoutBrowserSearch lists known model IDs without native browser
// search. Unknown IDs inherit tool support so newly released models do not
// lose requested tools (TS groq-browser-search-models.ts).
var modelsWithoutBrowserSearch = map[string]struct{}{
	"gemma2-9b-it":                                  {},
	"llama-3.1-8b-instant":                          {},
	"llama-3.3-70b-versatile":                       {},
	"meta-llama/llama-guard-4-12b":                  {},
	"deepseek-r1-distill-llama-70b":                 {},
	"meta-llama/llama-4-maverick-17b-128e-instruct": {},
	"meta-llama/llama-4-scout-17b-16e-instruct":     {},
	"meta-llama/llama-prompt-guard-2-22m":           {},
	"meta-llama/llama-prompt-guard-2-86m":           {},
	"moonshotai/kimi-k2-instruct-0905":              {},
	"qwen/qwen3.6-27b":                              {},
	"llama-guard-3-8b":                              {},
	"llama3-70b-8192":                               {},
	"llama3-8b-8192":                                {},
	"mixtral-8x7b-32768":                            {},
	"qwen-qwq-32b":                                  {},
	"qwen-2.5-32b":                                  {},
	"deepseek-r1-distill-qwen-32b":                  {},
}

// IsBrowserSearchSupportedModel reports whether the model may be sent the
// browser search tool: true for every model ID except the known exceptions.
func IsBrowserSearchSupportedModel(modelID string) bool {
	_, excluded := modelsWithoutBrowserSearch[modelID]
	return !excluded
}

// prepareTools converts unified tools and tool choice to Groq's wire format,
// mirroring TS groq-prepare-tools.ts. Unsupported provider tools are dropped
// with a warning.
func prepareTools(modelID string, tools []types.Tool, toolChoice types.ToolChoice) ([]map[string]interface{}, interface{}, []types.Warning) {
	if len(tools) == 0 {
		return nil, nil, nil
	}
	var warnings []types.Warning
	groqTools := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if t.Type == types.ToolTypeProviderDefined {
			if t.ProviderID == ToolIDBrowserSearch {
				if !IsBrowserSearchSupportedModel(modelID) {
					warnings = append(warnings, types.Warning{
						Type:    "unsupported",
						Feature: "provider-defined tool " + t.ProviderID,
						Details: "Browser search is not supported on model " + modelID + ".",
					})
				} else {
					groqTools = append(groqTools, map[string]interface{}{"type": "browser_search"})
				}
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "provider-defined tool " + t.ProviderID,
				})
			}
			continue
		}
		groqTools = append(groqTools, tool.ToJSONSchema(t))
	}
	if toolChoice.Type == "" {
		return groqTools, nil, warnings
	}
	return groqTools, tool.ConvertToolChoiceToOpenAI(toolChoice), warnings
}
