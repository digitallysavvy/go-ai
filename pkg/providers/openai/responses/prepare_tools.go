package responses

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// PrepareTools converts SDK tools to the OpenAI Responses API tool format.
// It dispatches on the tool name to determine the correct API representation:
//
//   - "openai.custom"       → CustomToolDef (name/description/format from ProviderOptions)
//   - "openai.local_shell"  → LocalShellToolDef {type: "local_shell"}
//   - "openai.shell"        → ShellToolDef {type: "shell", environment: ...}
//   - "openai.apply_patch"  → ApplyPatchToolDef {type: "apply_patch"}
//   - anything else         → FunctionToolDef {type: "function", ...}
//
// The returned slice is ready to be marshaled as the "tools" field in an
// OpenAI Responses API request body.
func PrepareTools(tools []types.Tool) []interface{} {
	if len(tools) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		def := convertTool(t)
		if def != nil {
			result = append(result, def)
		}
	}
	return result
}

// convertTool converts a single types.Tool to its Responses API representation.
func convertTool(t types.Tool) interface{} {
	// Custom tools are detected by ProviderOptions type since t.Name holds the
	// caller-assigned tool name (not the "openai.custom" sentinel).
	if _, ok := t.ProviderOptions.(openaitool.CustomTool); ok {
		return convertCustomTool(t)
	}
	toolID := t.ProviderID
	if toolID == "" {
		toolID = t.Name
	}
	switch toolID {
	case "openai.local_shell":
		return LocalShellToolDef{Type: "local_shell"}
	case "openai.shell":
		return convertShellTool(t)
	case "openai.apply_patch":
		return ApplyPatchToolDef{Type: "apply_patch"}
	case "openai.code_interpreter":
		return convertCodeInterpreterTool(t)
	case "openai.file_search":
		return convertFileSearchTool(t)
	case "openai.image_generation":
		return convertImageGenerationTool(t)
	case "openai.web_search":
		return convertWebSearchTool(t)
	case "openai.web_search_preview":
		return convertWebSearchPreviewTool(t)
	case "openai.mcp":
		return convertMCPTool(t)
	case "openai.tool_search":
		return convertToolSearchTool(t)
	default:
		if t.Type == types.ToolTypeProviderDefined {
			return nil
		}
		return convertFunctionTool(t)
	}
}

// convertCustomTool builds a CustomToolDef from a tool whose ProviderOptions
// holds an openaitool.CustomTool value.
// The tool name is taken from t.Name (set by the caller via ToTool("name")).
func convertCustomTool(t types.Tool) CustomToolDef {
	def := CustomToolDef{Type: "custom", Name: t.Name}

	ct, ok := t.ProviderOptions.(openaitool.CustomTool)
	if !ok {
		return def
	}

	def.Description = ct.Description

	if ct.Format != nil {
		f := &CustomToolDefFormat{Type: ct.Format.Type}
		if ct.Format.Syntax != nil {
			f.Syntax = ct.Format.Syntax
		}
		if ct.Format.Definition != nil {
			f.Definition = ct.Format.Definition
		}
		def.Format = f
	}

	return def
}

func convertCodeInterpreterTool(t types.Tool) map[string]interface{} {
	def := map[string]interface{}{"type": "code_interpreter"}
	cfg, _ := t.ProviderOptions.(openaitool.CodeInterpreterConfig)
	switch container := cfg.Container.(type) {
	case string:
		if container != "" {
			def["container"] = container
		} else {
			def["container"] = map[string]interface{}{"type": "auto"}
		}
	case openaitool.CodeInterpreterContainer:
		containerDef := map[string]interface{}{"type": "auto"}
		if len(container.FileIDs) > 0 {
			containerDef["file_ids"] = container.FileIDs
		}
		def["container"] = containerDef
	case *openaitool.CodeInterpreterContainer:
		containerDef := map[string]interface{}{"type": "auto"}
		if container != nil && len(container.FileIDs) > 0 {
			containerDef["file_ids"] = container.FileIDs
		}
		def["container"] = containerDef
	default:
		def["container"] = map[string]interface{}{"type": "auto"}
	}
	return def
}

func convertFileSearchTool(t types.Tool) map[string]interface{} {
	def := map[string]interface{}{"type": "file_search"}
	cfg, _ := t.ProviderOptions.(openaitool.FileSearchConfig)
	def["vector_store_ids"] = cfg.VectorStoreIDs
	if cfg.MaxNumResults != nil {
		def["max_num_results"] = *cfg.MaxNumResults
	}
	if cfg.Ranking != nil {
		ranking := map[string]interface{}{}
		if cfg.Ranking.Ranker != "" {
			ranking["ranker"] = cfg.Ranking.Ranker
		}
		if cfg.Ranking.ScoreThreshold != nil {
			ranking["score_threshold"] = *cfg.Ranking.ScoreThreshold
		}
		def["ranking_options"] = ranking
	}
	if cfg.Filters != nil {
		def["filters"] = cfg.Filters
	}
	return def
}

func convertImageGenerationTool(t types.Tool) map[string]interface{} {
	def := map[string]interface{}{"type": "image_generation"}
	cfg, _ := t.ProviderOptions.(openaitool.ImageGenerationConfig)
	if cfg.Background != "" {
		def["background"] = cfg.Background
	}
	if cfg.InputFidelity != "" {
		def["input_fidelity"] = cfg.InputFidelity
	}
	if cfg.InputImageMask != nil {
		mask := map[string]interface{}{}
		if cfg.InputImageMask.FileID != "" {
			mask["file_id"] = cfg.InputImageMask.FileID
		}
		if cfg.InputImageMask.ImageURL != "" {
			mask["image_url"] = cfg.InputImageMask.ImageURL
		}
		def["input_image_mask"] = mask
	}
	if cfg.Model != "" {
		def["model"] = cfg.Model
	}
	if cfg.Moderation != "" {
		def["moderation"] = cfg.Moderation
	}
	if cfg.OutputCompression != nil {
		def["output_compression"] = *cfg.OutputCompression
	}
	if cfg.OutputFormat != "" {
		def["output_format"] = cfg.OutputFormat
	}
	if cfg.PartialImages != nil {
		def["partial_images"] = *cfg.PartialImages
	}
	if cfg.Quality != "" {
		def["quality"] = cfg.Quality
	}
	if cfg.Size != "" {
		def["size"] = cfg.Size
	}
	return def
}

func convertWebSearchTool(t types.Tool) WebSearchToolDef {
	def := WebSearchToolDef{Type: "web_search"}
	cfg, _ := t.ProviderOptions.(openaitool.WebSearchConfig)
	if cfg.Filters != nil && len(cfg.Filters.AllowedDomains) > 0 {
		def.Filters = map[string]interface{}{"allowed_domains": cfg.Filters.AllowedDomains}
	}
	def.ExternalWebAccess = cfg.ExternalWebAccess
	def.SearchContextSize = cfg.SearchContextSize
	def.UserLocation = webSearchLocation(cfg.UserLocation)
	return def
}

func convertWebSearchPreviewTool(t types.Tool) WebSearchPreviewToolDef {
	def := WebSearchPreviewToolDef{Type: "web_search_preview"}
	cfg, _ := t.ProviderOptions.(openaitool.WebSearchPreviewConfig)
	def.SearchContextSize = cfg.SearchContextSize
	def.UserLocation = webSearchLocation(cfg.UserLocation)
	return def
}

func convertMCPTool(t types.Tool) map[string]interface{} {
	cfg, _ := t.ProviderOptions.(openaitool.MCPConfig)
	def := map[string]interface{}{
		"type":             "mcp",
		"server_label":     cfg.ServerLabel,
		"require_approval": "never",
	}
	if cfg.AllowedTools != nil {
		def["allowed_tools"] = mcpAllowedTools(cfg.AllowedTools)
	}
	if cfg.Authorization != "" {
		def["authorization"] = cfg.Authorization
	}
	if cfg.ConnectorID != "" {
		def["connector_id"] = cfg.ConnectorID
	}
	if len(cfg.Headers) > 0 {
		def["headers"] = cfg.Headers
	}
	if cfg.RequireApproval != nil {
		def["require_approval"] = mcpRequireApproval(cfg.RequireApproval)
	}
	if cfg.ServerDescription != "" {
		def["server_description"] = cfg.ServerDescription
	}
	if cfg.ServerURL != "" {
		def["server_url"] = cfg.ServerURL
	}
	return def
}

func mcpAllowedTools(value interface{}) interface{} {
	switch v := value.(type) {
	case []string:
		return v
	case openaitool.MCPAllowedTools:
		out := map[string]interface{}{}
		if v.ReadOnly != nil {
			out["read_only"] = *v.ReadOnly
		}
		if v.ToolNames != nil {
			out["tool_names"] = v.ToolNames
		}
		return out
	case *openaitool.MCPAllowedTools:
		if v == nil {
			return nil
		}
		return mcpAllowedTools(*v)
	default:
		return v
	}
}

func mcpRequireApproval(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return v
	case openaitool.MCPRequireApproval:
		if v.Never == nil {
			return "never"
		}
		never := map[string]interface{}{}
		if v.Never.ToolNames != nil {
			never["tool_names"] = v.Never.ToolNames
		}
		out := map[string]interface{}{"never": never}
		return out
	case *openaitool.MCPRequireApproval:
		if v == nil {
			return "never"
		}
		return mcpRequireApproval(*v)
	default:
		return v
	}
}

func webSearchLocation(loc *openaitool.WebSearchLocation) interface{} {
	if loc == nil {
		return nil
	}
	locationType := loc.Type
	if locationType == "" {
		locationType = "approximate"
	}
	out := map[string]interface{}{"type": locationType}
	if loc.Country != "" {
		out["country"] = loc.Country
	}
	if loc.City != "" {
		out["city"] = loc.City
	}
	if loc.Region != "" {
		out["region"] = loc.Region
	}
	if loc.Timezone != "" {
		out["timezone"] = loc.Timezone
	}
	return out
}

// convertToolSearchTool builds a ToolSearchToolDef from a tool_search tool.
func convertToolSearchTool(t types.Tool) ToolSearchToolDef {
	def := ToolSearchToolDef{Type: "tool_search"}

	opts, ok := t.ProviderOptions.(openaitool.ToolSearchOptions)
	if ok && opts.Execution != "" && opts.Execution != "server" {
		def.Execution = opts.Execution
	}

	if t.Description != "" {
		def.Description = t.Description
	}

	if t.Parameters != nil {
		if params, ok := t.Parameters.(map[string]interface{}); ok {
			def.Parameters = params
		}
	}

	return def
}

// convertShellTool builds a ShellToolDef, including the environment config
// from ProviderOptions if present.
func convertShellTool(t types.Tool) ShellToolDef {
	def := ShellToolDef{Type: "shell"}

	if t.ProviderOptions == nil {
		return def
	}

	env, ok := t.ProviderOptions.(*ShellEnvironment)
	if !ok {
		return def
	}

	toolEnv := &ShellToolDefEnvironment{Type: env.Type}

	if len(env.FileIDs) > 0 {
		toolEnv.FileIDs = env.FileIDs
	}
	if env.MemoryLimit != nil {
		toolEnv.MemoryLimit = env.MemoryLimit
	}
	if env.NetworkPolicy != nil {
		toolEnv.NetworkPolicy = env.NetworkPolicy
	}
	if len(env.Skills) > 0 {
		toolEnv.Skills = env.Skills
	}
	if env.ContainerID != nil {
		toolEnv.ContainerID = env.ContainerID
	}

	def.Environment = toolEnv
	return def
}

// convertFunctionTool builds a FunctionToolDef for a standard function tool.
func convertFunctionTool(t types.Tool) FunctionToolDef {
	def := FunctionToolDef{
		Type:        "function",
		Name:        t.Name,
		Description: t.Description,
		Parameters:  defaultFunctionParameters(t.Parameters),
	}

	if t.Strict {
		strict := true
		def.Strict = &strict
	}
	if deferLoading, ok := functionToolDeferLoading(t.ProviderOptions); ok {
		def.DeferLoading = &deferLoading
	}

	return def
}

func functionToolDeferLoading(providerOptions interface{}) (bool, bool) {
	options, ok := providerOptions.(map[string]interface{})
	if !ok {
		return false, false
	}
	openaiRaw, ok := options["openai"]
	if !ok {
		return false, false
	}
	openaiOptions, ok := openaiRaw.(map[string]interface{})
	if !ok {
		return false, false
	}
	deferLoading, ok := openaiOptions["deferLoading"].(bool)
	return deferLoading, ok
}

func defaultFunctionParameters(parameters interface{}) interface{} {
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
