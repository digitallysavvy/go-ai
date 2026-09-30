package cohere

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// prepareCohereTools converts unified tools/tool-choice to Cohere's wire
// format. It mirrors the TypeScript SDK's cohere-prepare-tools.ts prepareTools
// exactly, including which tool_choice values map to Cohere's "NONE"/"REQUIRED"
// strings and the tool-name filtering behavior for a forced single-tool
// choice.
//
// hasToolChoice distinguishes an explicitly-set ToolChoice (opts.ToolChoice.Type
// != "") from the zero value, matching TS's `toolChoice == null` check (Go's
// types.ToolChoice is a plain struct, not a pointer, so callers must signal
// "unset" separately).
func prepareCohereTools(tools []types.Tool, toolChoice types.ToolChoice, hasToolChoice bool) ([]map[string]interface{}, interface{}, []types.Warning) {
	// When the tools slice is empty, treat it like TS's `tools?.length ? tools : undefined`:
	// no tools key at all, and tool choice is ignored entirely.
	if len(tools) == 0 {
		return nil, nil, nil
	}

	var warnings []types.Warning
	cohereTools := make([]map[string]interface{}, 0, len(tools))

	for _, t := range tools {
		if t.Type == types.ToolTypeProviderDefined {
			id := t.ProviderID
			if id == "" {
				id = t.Name
			}
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("provider-defined tool %s", id),
			})
			continue
		}

		fn := map[string]interface{}{
			"name": t.Name,
		}
		if t.Description != "" {
			fn["description"] = t.Description
		}
		fn["parameters"] = t.Parameters

		cohereTools = append(cohereTools, map[string]interface{}{
			"type":     "function",
			"function": fn,
		})
	}

	if !hasToolChoice {
		return cohereTools, nil, warnings
	}

	switch toolChoice.Type {
	case types.ToolChoiceAuto:
		return cohereTools, nil, warnings

	case types.ToolChoiceNone:
		return cohereTools, "NONE", warnings

	case types.ToolChoiceRequired:
		return cohereTools, "REQUIRED", warnings

	case types.ToolChoiceTool:
		filtered := make([]map[string]interface{}, 0, len(cohereTools))
		for _, ct := range cohereTools {
			fn, ok := ct["function"].(map[string]interface{})
			if !ok {
				continue
			}
			if name, _ := fn["name"].(string); name == toolChoice.ToolName {
				filtered = append(filtered, ct)
			}
		}
		return filtered, "REQUIRED", warnings

	default:
		return cohereTools, nil, warnings
	}
}
