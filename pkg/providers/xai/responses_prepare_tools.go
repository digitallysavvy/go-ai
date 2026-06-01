package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// prepareXAIResponsesTools converts SDK tools to XAI Responses API wire format.
// XAI provider tools are serialized using their ProviderOptions config.
// Regular function tools become {type:"function", name, description, parameters}.
func prepareXAIResponsesTools(tools []types.Tool) []interface{} {
	if len(tools) == 0 {
		return nil
	}
	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		result = append(result, convertXAIResponsesTool(t))
	}
	return result
}

func convertXAIResponsesTool(t types.Tool) interface{} {
	switch t.Name {
	case "xai.web_search":
		cfg, _ := t.ProviderOptions.(WebSearchConfig)
		m := map[string]interface{}{"type": "web_search"}
		if len(cfg.AllowedDomains) > 0 {
			m["allowed_domains"] = cfg.AllowedDomains
		}
		if len(cfg.ExcludedDomains) > 0 {
			m["excluded_domains"] = cfg.ExcludedDomains
		}
		if cfg.EnableImageSearch != nil {
			m["enable_image_search"] = *cfg.EnableImageSearch
		}
		if cfg.EnableImageUnderstanding != nil {
			m["enable_image_understanding"] = *cfg.EnableImageUnderstanding
		}
		return m

	case "xai.x_search":
		cfg, _ := t.ProviderOptions.(XSearchConfig)
		m := map[string]interface{}{"type": "x_search"}
		if len(cfg.AllowedXHandles) > 0 {
			m["allowed_x_handles"] = cfg.AllowedXHandles
		}
		if len(cfg.ExcludedXHandles) > 0 {
			m["excluded_x_handles"] = cfg.ExcludedXHandles
		}
		if cfg.FromDate != nil {
			m["from_date"] = *cfg.FromDate
		}
		if cfg.ToDate != nil {
			m["to_date"] = *cfg.ToDate
		}
		if cfg.EnableImageUnderstanding != nil {
			m["enable_image_understanding"] = *cfg.EnableImageUnderstanding
		}
		if cfg.EnableVideoUnderstanding != nil {
			m["enable_video_understanding"] = *cfg.EnableVideoUnderstanding
		}
		return m

	case "xai.code_execution":
		// IMPORTANT: xai.code_execution maps to API type "code_interpreter" (not "code_execution").
		return map[string]interface{}{"type": "code_interpreter"}

	case "xai.view_image":
		return map[string]interface{}{"type": "view_image"}

	case "xai.view_x_video":
		return map[string]interface{}{"type": "view_x_video"}

	case "xai.file_search":
		cfg, _ := t.ProviderOptions.(FileSearchOptions)
		m := map[string]interface{}{"type": "file_search"}
		if len(cfg.VectorStoreIDs) > 0 {
			m["vector_store_ids"] = cfg.VectorStoreIDs
		}
		if cfg.MaxNumResults > 0 {
			m["max_num_results"] = cfg.MaxNumResults
		}
		return m

	case "xai.mcp":
		cfg, _ := t.ProviderOptions.(MCPServerOptions)
		m := map[string]interface{}{"type": "mcp", "server_url": cfg.ServerURL}
		if cfg.ServerLabel != "" {
			m["server_label"] = cfg.ServerLabel
		}
		if cfg.ServerDescription != "" {
			m["server_description"] = cfg.ServerDescription
		}
		if len(cfg.AllowedTools) > 0 {
			m["allowed_tools"] = cfg.AllowedTools
		}
		if len(cfg.Headers) > 0 {
			m["headers"] = cfg.Headers
		}
		if cfg.Authorization != "" {
			m["authorization"] = cfg.Authorization
		}
		return m

	default:
		// Regular function tool
		return map[string]interface{}{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"parameters":  stripAdditionalPropertiesFalse(t.Parameters),
		}
	}
}
