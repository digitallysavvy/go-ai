package google

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func prepareInteractionsTools(tools []types.Tool, toolChoice types.ToolChoice) ([]map[string]interface{}, interface{}, []types.Warning) {
	if len(tools) == 0 {
		return nil, nil, nil
	}
	out := make([]map[string]interface{}, 0, len(tools))
	var warnings []types.Warning
	hasFunction := false
	for _, t := range tools {
		if t.Type == "" || t.Type == types.ToolTypeFunction || t.Type == types.ToolTypeDynamic {
			hasFunction = true
			out = append(out, pruneMap(map[string]interface{}{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			}))
			continue
		}
		if t.Type == types.ToolTypeProviderDefined || t.Type == "provider" {
			mapped, warning := mapInteractionsProviderTool(t)
			if warning != nil {
				warnings = append(warnings, *warning)
				continue
			}
			out = append(out, mapped)
			continue
		}
		msg := fmt.Sprintf("Only function tools and google.* provider-defined tools are supported by google.interactions; tool dropped.")
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "tool of type " + t.Type, Details: msg, Message: msg})
	}
	var choice interface{}
	if hasFunction && toolChoice.Type != "" {
		switch toolChoice.Type {
		case types.ToolChoiceAuto:
			choice = "auto"
		case types.ToolChoiceRequired:
			choice = "any"
		case types.ToolChoiceNone:
			choice = "none"
		case types.ToolChoiceTool:
			choice = map[string]interface{}{
				"allowed_tools": map[string]interface{}{
					"mode":  "validated",
					"tools": []string{toolChoice.ToolName},
				},
			}
		}
	}
	if len(out) == 0 {
		return nil, nil, warnings
	}
	return out, choice, warnings
}

func mapInteractionsProviderTool(t types.Tool) (map[string]interface{}, *types.Warning) {
	args := t.ProviderArgs
	if args == nil {
		args = map[string]interface{}{}
	}
	switch t.ProviderID {
	case "google.google_search":
		tool := map[string]interface{}{"type": "google_search"}
		if st, ok := args["searchTypes"].(map[string]interface{}); ok {
			var searchTypes []string
			if st["webSearch"] != nil {
				searchTypes = append(searchTypes, "web_search")
			}
			if st["imageSearch"] != nil {
				searchTypes = append(searchTypes, "image_search")
			}
			if len(searchTypes) > 0 {
				tool["search_types"] = searchTypes
			}
		}
		return tool, nil
	case "google.code_execution":
		return map[string]interface{}{"type": "code_execution"}, nil
	case "google.url_context":
		return map[string]interface{}{"type": "url_context"}, nil
	case "google.file_search":
		return pruneMap(map[string]interface{}{
			"type":                    "file_search",
			"file_search_store_names": args["fileSearchStoreNames"],
			"top_k":                   args["topK"],
			"metadata_filter":         args["metadataFilter"],
		}), nil
	case "google.google_maps":
		return pruneMap(map[string]interface{}{
			"type":          "google_maps",
			"latitude":      args["latitude"],
			"longitude":     args["longitude"],
			"enable_widget": args["enableWidget"],
		}), nil
	case "google.computer_use":
		environment, _ := args["environment"].(string)
		if environment == "" {
			environment = "browser"
		}
		return pruneMap(map[string]interface{}{
			"type":                        "computer_use",
			"environment":                 environment,
			"excludedPredefinedFunctions": args["excludedPredefinedFunctions"],
		}), nil
	case "google.mcp_server":
		return pruneMap(map[string]interface{}{
			"type":          "mcp_server",
			"name":          args["name"],
			"url":           args["url"],
			"headers":       args["headers"],
			"allowed_tools": args["allowedTools"],
		}), nil
	case "google.retrieval":
		retrievalTypes := args["retrievalTypes"]
		if retrievalTypes == nil {
			retrievalTypes = []string{"vertex_ai_search"}
		}
		return pruneMap(map[string]interface{}{
			"type":                    "retrieval",
			"retrieval_types":         retrievalTypes,
			"vertex_ai_search_config": args["vertexAiSearchConfig"],
		}), nil
	default:
		msg := fmt.Sprintf("provider-defined tool %s is not supported by google.interactions; tool dropped.", t.ProviderID)
		return nil, &types.Warning{Type: "unsupported", Feature: "provider-defined tool " + t.ProviderID, Details: msg, Message: msg}
	}
}
