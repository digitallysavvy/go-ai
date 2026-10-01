package perplexity

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// preparePerplexityTools mirrors TS perplexity-prepare-tools.ts: it converts
// AI SDK function tools into Agent API {type:"function", ...} definitions.
// Provider-defined tools are not supported by this conversion (native Agent
// API tools are supplied separately via providerOptions.perplexity.tools) and
// produce an "unsupported" warning instead. toolChoice is also unsupported --
// the Agent API always selects tools automatically.
func preparePerplexityTools(tools []types.Tool, toolChoice types.ToolChoice) ([]map[string]interface{}, []types.Warning) {
	var prepared []map[string]interface{}
	var warnings []types.Warning

	for _, tool := range tools {
		if tool.Type == types.ToolTypeProviderDefined {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("provider-defined tool %s", tool.Name),
			})
			continue
		}

		def := map[string]interface{}{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
		if tool.Strict != nil {
			def["strict"] = *tool.Strict
		}
		prepared = append(prepared, def)
	}

	if toolChoice.Type != "" && toolChoice.Type != types.ToolChoiceAuto {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "toolChoice",
			Details: "The Perplexity Agent API currently selects tools automatically.",
		})
	}

	return prepared, warnings
}
