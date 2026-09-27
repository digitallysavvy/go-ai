package responses

import (
	"fmt"
	"strings"

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
	result, _ := PrepareToolsWithError(tools)
	return result
}

// PrepareToolsWithError converts SDK tools to OpenAI Responses API tool
// definitions and reports TS-parity unsupported functionality errors.
func PrepareToolsWithError(tools []types.Tool) ([]interface{}, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	result := make([]interface{}, 0, len(tools))
	namespaces := map[string]*NamespaceToolDef{}
	for _, t := range tools {
		def := convertTool(t)
		if def != nil {
			if functionDef, ok := def.(FunctionToolDef); ok {
				namespace, ok := functionToolNamespace(t.ProviderOptions)
				if ok {
					namespaceDef := namespaces[namespace.Name]
					if namespaceDef == nil {
						namespaceDef = &NamespaceToolDef{
							Type:        "namespace",
							Name:        namespace.Name,
							Description: namespace.Description,
							Tools:       []FunctionToolDef{},
						}
						namespaces[namespace.Name] = namespaceDef
						result = append(result, namespaceDef)
					} else if namespaceDef.Description != namespace.Description {
						return nil, fmt.Errorf("unsupported functionality: conflicting descriptions for OpenAI tool namespace %q", namespace.Name)
					}
					namespaceDef.Tools = append(namespaceDef.Tools, functionDef)
					continue
				}
			}
			result = append(result, def)
		}
	}
	return result, nil
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
	case "openai.computer":
		return map[string]interface{}{"type": "computer"}
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
	case "openai.programmatic_tool_calling":
		return map[string]interface{}{"type": "programmatic_tool_calling"}
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
	if ct.Async != nil {
		def.Async = ct.Async
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
	if cfg.Action != "" {
		def["action"] = cfg.Action
	}
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
	if cfg.Filters != nil && (len(cfg.Filters.AllowedDomains) > 0 || len(cfg.Filters.BlockedDomains) > 0) {
		filters := map[string]interface{}{}
		if len(cfg.Filters.AllowedDomains) > 0 {
			filters["allowed_domains"] = cfg.Filters.AllowedDomains
		}
		if len(cfg.Filters.BlockedDomains) > 0 {
			filters["blocked_domains"] = cfg.Filters.BlockedDomains
		}
		def.Filters = filters
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
	// Row 4a09793: async is read here without model-capability gating --
	// the caller (responses_language_model.go, which knows the model id)
	// is responsible for warning and stripping it when unsupported, since
	// this package has no model ID to check against.
	if async, ok := functionToolAsync(t.ProviderOptions); ok {
		def.Async = &async
	}
	if allowedCallers, ok := functionToolAllowedCallers(t.ProviderOptions); ok {
		def.AllowedCallers = allowedCallers
	}
	if outputSchema, ok := functionToolOutputSchema(t.ProviderOptions); ok {
		def.OutputSchema = outputSchema
	}

	return def
}

func functionToolDeferLoading(providerOptions interface{}) (bool, bool) {
	openaiOptions, ok := functionToolOpenAIOptions(providerOptions)
	if !ok {
		return false, false
	}
	deferLoading, ok := openaiOptions["deferLoading"].(bool)
	return deferLoading, ok
}

func functionToolAsync(providerOptions interface{}) (bool, bool) {
	openaiOptions, ok := functionToolOpenAIOptions(providerOptions)
	if !ok {
		return false, false
	}
	async, ok := openaiOptions["async"].(bool)
	return async, ok
}

func functionToolAllowedCallers(providerOptions interface{}) ([]string, bool) {
	openaiOptions, ok := functionToolOpenAIOptions(providerOptions)
	if !ok {
		return nil, false
	}
	raw, ok := openaiOptions["allowedCallers"]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []string:
		return v, len(v) > 0
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func functionToolOutputSchema(providerOptions interface{}) (interface{}, bool) {
	openaiOptions, ok := functionToolOpenAIOptions(providerOptions)
	if !ok {
		return nil, false
	}
	schema, ok := openaiOptions["outputSchema"]
	return schema, ok && schema != nil
}

type functionToolNamespaceOption struct {
	Name        string
	Description string
}

func functionToolNamespace(providerOptions interface{}) (functionToolNamespaceOption, bool) {
	openaiOptions, ok := functionToolOpenAIOptions(providerOptions)
	if !ok {
		return functionToolNamespaceOption{}, false
	}
	rawNamespace, ok := openaiOptions["namespace"]
	if !ok || rawNamespace == nil {
		return functionToolNamespaceOption{}, false
	}
	namespace, ok := rawNamespace.(map[string]interface{})
	if !ok {
		return functionToolNamespaceOption{}, false
	}
	name, _ := namespace["name"].(string)
	description, _ := namespace["description"].(string)
	if name == "" {
		return functionToolNamespaceOption{}, false
	}
	return functionToolNamespaceOption{Name: name, Description: description}, true
}

func functionToolOpenAIOptions(providerOptions interface{}) (map[string]interface{}, bool) {
	options, ok := providerOptions.(map[string]interface{})
	if !ok {
		return nil, false
	}
	openaiRaw, ok := options["openai"]
	if !ok {
		return nil, false
	}
	openaiOptions, ok := openaiRaw.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return openaiOptions, true
}

// allowedToolResolution mirrors TS AllowedToolResolution: a requested
// allowedTools name either resolves to a concrete tool_choice.allowed_tools
// entry, or is unsupported (with a reason) and gets dropped with a warning.
type allowedToolResolution struct {
	supported bool
	entry     AllowedToolsToolEntry
	reason    string
}

// resolveAllowedToolForTool determines the tool_choice.allowed_tools entry
// shape for a single SDK tool, mirroring TS `toAllowedToolResolution` plus
// the "custom" and "function" cases from `openai-responses-prepare-tools.ts`.
func resolveAllowedToolForTool(t types.Tool) allowedToolResolution {
	if _, ok := t.ProviderOptions.(openaitool.CustomTool); ok {
		return allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: "custom", Name: t.Name}}
	}
	toolID := t.ProviderID
	if toolID == "" {
		toolID = t.Name
	}
	switch toolID {
	case "openai.mcp":
		cfg, _ := t.ProviderOptions.(openaitool.MCPConfig)
		return allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: "mcp", ServerLabel: cfg.ServerLabel}}
	case "openai.file_search", "openai.web_search", "openai.web_search_preview",
		"openai.image_generation", "openai.code_interpreter", "openai.computer",
		"openai.apply_patch", "openai.shell", "openai.local_shell",
		"openai.programmatic_tool_calling":
		return allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: normalizeOpenAIToolName(toolID)}}
	}
	if t.Type == types.ToolTypeProviderDefined {
		return allowedToolResolution{
			supported: false,
			reason:    fmt.Sprintf("OpenAI does not support %s tools in tool_choice.allowed_tools", toolID),
		}
	}
	if _, ok := functionToolNamespace(t.ProviderOptions); ok {
		return allowedToolResolution{
			supported: false,
			reason:    "tools inside an OpenAI tool namespace are not visible to tool_choice.allowed_tools",
		}
	}
	if deferLoading, ok := functionToolDeferLoading(t.ProviderOptions); ok && deferLoading {
		return allowedToolResolution{
			supported: false,
			reason:    "deferred tools are not visible to tool_choice.allowed_tools",
		}
	}
	return allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: "function", Name: t.Name}}
}

// ResolveAllowedTools implements the TS openai-responses-prepare-tools.ts
// `allowedTools` handling (row a062795): each requested tool name is
// resolved against the actual tool list by exact name match to determine
// whether it needs a "function", "custom", "mcp" (by server_label), or
// built-in provider tool_choice.allowed_tools entry — sending the wrong
// shape (e.g. `{type:"function"}` for an actual built-in web_search tool) is
// rejected by the API. Unknown names are sent through as a function tool
// with a warning; unsupported tools (namespaced/deferred/provider-defined
// without an allow-list entry) are dropped with a warning. If every
// requested name is dropped, this returns an error (TS throws
// UnsupportedFunctionalityError).
//
// Note: unlike TS, this does not implement the provider-tool-name alias
// resolution layer (matching an allowedTools entry against a tool's
// *mapped* wire name in addition to its SDK name) — Go's tool model has no
// separate name-mapping layer, so direct SDK tool name matching is the only
// resolution path.
func ResolveAllowedTools(tools []types.Tool, toolNames []string, mode string) (*AllowedToolsToolChoice, []types.Warning, error) {
	if len(toolNames) == 0 {
		return nil, nil, nil
	}
	if mode == "" {
		mode = "auto"
	}

	resolutions := make(map[string]allowedToolResolution, len(tools))
	for _, t := range tools {
		resolutions[t.Name] = resolveAllowedToolForTool(t)
	}

	var warnings []types.Warning
	var entries []AllowedToolsToolEntry
	var dropped []string
	for _, name := range toolNames {
		resolution, ok := resolutions[name]
		if !ok {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("allowedTools entry %q", name),
				Details: "the tool is not part of the tools for this request and is sent as a function tool",
			})
			entries = append(entries, AllowedToolsToolEntry{Type: "function", Name: name})
			continue
		}
		if !resolution.supported {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("allowedTools entry %q", name),
				Details: resolution.reason + "; the tool is removed from the allowed tools",
			})
			dropped = append(dropped, name)
			continue
		}
		entries = append(entries, resolution.entry)
	}

	if len(entries) == 0 {
		return nil, warnings, fmt.Errorf(
			"unsupported functionality: allowedTools with only tools that cannot be allow-listed (%s)",
			strings.Join(dropped, ", "),
		)
	}

	return &AllowedToolsToolChoice{Type: "allowed_tools", Mode: mode, Tools: entries}, warnings, nil
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
