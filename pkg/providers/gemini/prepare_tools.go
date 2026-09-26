package gemini

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// preparedTools is the result of prepareTools.
type preparedTools struct {
	tools      []map[string]interface{}
	toolConfig map[string]interface{}
	warnings   []types.Warning
}

func unsupportedToolWarning(feature, details string) types.Warning {
	w := types.Warning{Type: "unsupported", Feature: feature, Details: details}
	if details != "" {
		w.Message = details
	} else {
		w.Message = feature
	}
	return w
}

// functionDeclaration mirrors TS prepareFunctionDeclaration: the JSON Schema
// is sent as-is under parametersJsonSchema (no OpenAPI conversion).
func functionDeclaration(t types.Tool) map[string]interface{} {
	return map[string]interface{}{
		"name":                 t.Name,
		"description":          t.Description,
		"parametersJsonSchema": defaultToolSchema(t.Parameters),
	}
}

func defaultToolSchema(parameters interface{}) interface{} {
	if parameters == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return parameters
}

// prepareTools ports TS prepareTools (google-prepare-tools.ts).
func prepareTools(tools []types.Tool, toolChoice types.ToolChoice, modelID string, isVertex bool) preparedTools {
	var out preparedTools
	if len(tools) == 0 {
		return out
	}
	caps := GetModelCapabilities(modelID)

	hasFunctionTools, hasProviderTools := false, false
	for _, t := range tools {
		if t.Type == "provider" {
			hasProviderTools = true
		} else {
			hasFunctionTools = true
		}
	}

	if hasFunctionTools && hasProviderTools && !caps.UsesGemini3Features {
		out.warnings = append(out.warnings, unsupportedToolWarning("combination of function and provider-defined tools", ""))
	}

	if hasProviderTools {
		var googleTools []map[string]interface{}
		for _, t := range tools {
			if t.Type != "provider" {
				continue
			}
			feature := "provider-defined tool " + t.ProviderID
			gate, details := caps.SupportsGemini2Tools, ""
			switch t.ProviderID {
			case "google.google_search":
				details = "Google Search requires Gemini 2.0 or newer."
			case "google.enterprise_web_search":
				details = "Enterprise Web Search requires Gemini 2.0 or newer."
			case "google.url_context":
				details = "The URL context tool is not supported with other Gemini models than Gemini 2."
			case "google.code_execution":
				details = "The code execution tool is not supported with other Gemini models than Gemini 2."
			case "google.file_search":
				gate = caps.SupportsFileSearch
				details = "The file search tool is only supported with Gemini 2.5 models and Gemini 3 models."
			case "google.vertex_rag_store":
				details = "The RAG store tool is not supported with other Gemini models than Gemini 2."
			case "google.google_maps":
				details = "The Google Maps grounding tool is not supported with Gemini models other than Gemini 2 or newer."
			default:
				out.warnings = append(out.warnings, unsupportedToolWarning(feature, ""))
				continue
			}
			if !gate {
				out.warnings = append(out.warnings, unsupportedToolWarning(feature, details))
				continue
			}
			if entry := buildNativeToolEntry(t); entry != nil {
				googleTools = append(googleTools, entry)
			}
		}

		if hasFunctionTools && caps.UsesGemini3Features && len(googleTools) > 0 {
			var decls []map[string]interface{}
			for _, t := range tools {
				if t.Type != "provider" {
					decls = append(decls, functionDeclaration(t))
				}
			}
			fcc := map[string]interface{}{"mode": "VALIDATED"}
			switch toolChoice.Type {
			case types.ToolChoiceNone:
				fcc = map[string]interface{}{"mode": "NONE"}
			case types.ToolChoiceRequired:
				fcc = map[string]interface{}{"mode": "ANY"}
			case types.ToolChoiceTool:
				fcc = map[string]interface{}{"mode": "ANY", "allowedFunctionNames": []string{toolChoice.ToolName}}
			}
			out.toolConfig = map[string]interface{}{"functionCallingConfig": fcc}
			if !isVertex {
				out.toolConfig["includeServerSideToolInvocations"] = true
			}
			out.tools = append(googleTools, map[string]interface{}{"functionDeclarations": decls})
			return out
		}

		if len(googleTools) > 0 {
			out.tools = googleTools
		}
		return out
	}

	var decls []map[string]interface{}
	hasStrictTools := false
	for _, t := range tools {
		decls = append(decls, functionDeclaration(t))
		if t.Strict {
			hasStrictTools = true
		}
	}
	out.tools = []map[string]interface{}{{"functionDeclarations": decls}}

	switch toolChoice.Type {
	case "":
		if hasStrictTools {
			out.toolConfig = map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "VALIDATED"}}
		}
	case types.ToolChoiceNone:
		out.toolConfig = map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "NONE"}}
	case types.ToolChoiceRequired:
		// Forced tool choices keep ANY even with strict tools (TS 8e90283).
		out.toolConfig = map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "ANY"}}
	case types.ToolChoiceTool:
		out.toolConfig = map[string]interface{}{"functionCallingConfig": map[string]interface{}{
			"mode": "ANY", "allowedFunctionNames": []string{toolChoice.ToolName},
		}}
	default: // auto
		mode := "AUTO"
		if hasStrictTools {
			mode = "VALIDATED"
		}
		out.toolConfig = map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": mode}}
	}
	return out
}
