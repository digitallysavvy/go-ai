package moonshot

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// moonshotFunctionTool is the wire shape Moonshot expects for a function tool
// definition. Mirrors TS MoonshotAIFunctionTool.
type moonshotFunctionTool struct {
	Type     string                     `json:"type"`
	Function moonshotFunctionToolFields `json:"function"`
}

type moonshotFunctionToolFields struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters"`
	Strict      *bool       `json:"strict,omitempty"`
}

// prepareMoonshotTools converts SDK tools/tool-choice to Moonshot's wire
// format. Mirrors TS prepareTools in moonshotai-prepare-tools.ts: normalizes
// tool parameter schemas for MFJS, warns on provider-defined tools (Moonshot
// has none), and omits "required" tool choice (with a warning) for the Kimi
// K2.6/K2.7 models that reject it.
func prepareMoonshotTools(tools []types.Tool, toolChoice types.ToolChoice, hasToolChoice bool, modelID string) ([]moonshotFunctionTool, interface{}, []types.Warning, error) {
	if len(tools) == 0 {
		return nil, nil, nil, nil
	}

	var warnings []types.Warning
	moonshotTools := make([]moonshotFunctionTool, 0, len(tools))

	for _, tool := range tools {
		if tool.ProviderExecuted {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "provider-defined tool " + tool.Name,
			})
			continue
		}

		normalized, err := NormalizeJSONSchemaForMFJS(moonshotDefaultObjectSchema(tool.Parameters))
		if err != nil {
			return nil, nil, nil, err
		}

		fn := moonshotFunctionToolFields{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  normalized,
		}
		if tool.Strict {
			strict := true
			fn.Strict = &strict
		}
		moonshotTools = append(moonshotTools, moonshotFunctionTool{Type: "function", Function: fn})
	}

	if !hasToolChoice {
		return moonshotTools, nil, warnings, nil
	}

	switch toolChoice.Type {
	case types.ToolChoiceAuto:
		return moonshotTools, "auto", warnings, nil
	case types.ToolChoiceNone:
		return moonshotTools, "none", warnings, nil
	case types.ToolChoiceRequired:
		if modelID == "kimi-k2.6" || modelID == "kimi-k2.7-code" || modelID == "kimi-k2.7-code-highspeed" {
			details := `Moonshot AI rejects required tool choice for this model. The setting has been omitted; use "auto" or select a specific tool instead.`
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: `tool choice "required" for model "` + modelID + `"`,
				Details: details,
				Message: details,
			})
			return moonshotTools, nil, warnings, nil
		}
		return moonshotTools, "required", warnings, nil
	case types.ToolChoiceTool:
		return moonshotTools, map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": toolChoice.ToolName},
		}, warnings, nil
	default:
		return moonshotTools, nil, warnings, nil
	}
}

// moonshotDefaultObjectSchema mirrors the tool package's defaultObjectSchema:
// nil parameters become an empty object schema; a schema missing "type" gets
// "object" injected so the MFJS root-object check passes.
func moonshotDefaultObjectSchema(parameters interface{}) interface{} {
	if parameters == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	if schema, ok := parameters.(map[string]interface{}); ok {
		if _, hasType := schema["type"]; !hasType {
			copied := make(map[string]interface{}, len(schema)+1)
			for key, value := range schema {
				copied[key] = value
			}
			copied["type"] = "object"
			return copied
		}
	}
	return parameters
}
