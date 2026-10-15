package xai

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// validateXAIResponsesToolArgs mirrors the zod checks TS runs on provider tool
// args with validateTypes before building the request: web search takes at
// most 5 allowed and 5 excluded domains, X search at most 20 allowed and 20
// excluded handles, and image generation `action` is auto, generate or edit.
func validateXAIResponsesToolArgs(tools []types.Tool) error {
	tooBig := func(tool, field string, limit int) error {
		return providererrors.NewValidationError("tools",
			fmt.Sprintf("Type validation failed for %s: %s: Too big: expected array to have <=%d items", tool, field, limit), nil)
	}
	for _, t := range tools {
		switch t.Name {
		case "xai.web_search":
			cfg, _ := t.ProviderOptions.(WebSearchConfig)
			if len(cfg.AllowedDomains) > 5 {
				return tooBig(t.Name, "allowedDomains", 5)
			}
			if len(cfg.ExcludedDomains) > 5 {
				return tooBig(t.Name, "excludedDomains", 5)
			}
		case "xai.x_search":
			cfg, _ := t.ProviderOptions.(XSearchConfig)
			if len(cfg.AllowedXHandles) > 20 {
				return tooBig(t.Name, "allowedXHandles", 20)
			}
			if len(cfg.ExcludedXHandles) > 20 {
				return tooBig(t.Name, "excludedXHandles", 20)
			}
		case "xai.image_generation":
			cfg, _ := t.ProviderOptions.(ImageGenerationConfig)
			switch cfg.Action {
			case "", "auto", "generate", "edit":
			default:
				return providererrors.NewValidationError("tools",
					fmt.Sprintf("Type validation failed for %s: action: Invalid option: expected one of \"auto\"|\"generate\"|\"edit\"", t.Name), nil)
			}
		}
	}
	return nil
}

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

	case "xai.image_generation":
		cfg, _ := t.ProviderOptions.(ImageGenerationConfig)
		m := map[string]interface{}{"type": "image_generation"}
		if cfg.Action != "" {
			m["action"] = cfg.Action
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
		// Regular function tool. Parameters are sent unchanged, including any
		// `additionalProperties: false` in the JSON schema (row 6e405ae:
		// xAI's upstream removal of the additionalProperties flag was itself
		// reverted, so the SDK must stop stripping it).
		m := map[string]interface{}{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		}
		// TS xai-responses-prepare-tools.ts: `...(tool.strict != null ?
		// {strict: tool.strict} : {})` — forward the explicit value,
		// including `false`.
		if t.Strict != nil {
			m["strict"] = *t.Strict
		}
		return m
	}
}
