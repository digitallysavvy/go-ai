package huggingface

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// hfTool mirrors TS HuggingFaceResponsesTool.
type hfTool struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters"`
}

// prepareResponsesTools mirrors TS prepareResponsesTools. toolsPresent
// reports whether the "tools" key should be included in the request body at
// all (true whenever the input tools slice was non-empty, even if every tool
// turned out to be a provider-defined tool that could not be encoded --
// mirroring TS's `tools: huggingfaceTools` always being set, even to an
// empty array, once `tools?.length` was truthy).
func prepareResponsesTools(tools []types.Tool, toolChoice types.ToolChoice) (hfTools []hfTool, toolsPresent bool, mappedToolChoice interface{}, warnings []types.Warning) {
	if len(tools) == 0 {
		return nil, false, nil, nil
	}

	toolsPresent = true
	hfTools = []hfTool{}

	for _, tool := range tools {
		switch tool.Type {
		case types.ToolTypeProviderDefined:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("provider-defined tool %s", tool.ProviderID),
			})
		default:
			hfTools = append(hfTools, hfTool{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			})
		}
	}

	switch toolChoice.Type {
	case types.ToolChoiceAuto:
		mappedToolChoice = "auto"
	case types.ToolChoiceRequired:
		mappedToolChoice = "required"
	case types.ToolChoiceTool:
		mappedToolChoice = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": toolChoice.ToolName,
			},
		}
		// ToolChoiceNone and the zero value are not supported and are ignored,
		// matching TS's `case 'none': // not supported, ignore`.
	}

	return hfTools, toolsPresent, mappedToolChoice, warnings
}
