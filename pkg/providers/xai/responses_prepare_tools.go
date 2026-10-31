package xai

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// xaiToolArgsNodes are the TS args schemas prepareResponsesTools validates
// with validateTypes (xai/src/tool/*.ts: webSearchArgsSchema,
// xSearchArgsSchema, imageGenerationArgsSchema, fileSearchArgsSchema,
// mcpServerArgsSchema), with the .max(n) limits: at most 5 web search
// domains and 20 X handles (#22225).
var xaiToolArgsNodes = map[string]zodlite.Node{
	"xai.web_search": zodlite.Object(
		zodlite.F("allowedDomains", zodlite.Optional(zodlite.MaxItems(zodlite.Array(zodlite.String), 5))),
		zodlite.F("excludedDomains", zodlite.Optional(zodlite.MaxItems(zodlite.Array(zodlite.String), 5))),
		zodlite.F("enableImageSearch", zodlite.Optional(zodlite.Boolean)),
		zodlite.F("enableImageUnderstanding", zodlite.Optional(zodlite.Boolean)),
	),
	"xai.x_search": zodlite.Object(
		zodlite.F("allowedXHandles", zodlite.Optional(zodlite.MaxItems(zodlite.Array(zodlite.String), 20))),
		zodlite.F("excludedXHandles", zodlite.Optional(zodlite.MaxItems(zodlite.Array(zodlite.String), 20))),
		zodlite.F("fromDate", zodlite.Optional(zodlite.String)),
		zodlite.F("toDate", zodlite.Optional(zodlite.String)),
		zodlite.F("enableImageUnderstanding", zodlite.Optional(zodlite.Boolean)),
		zodlite.F("enableVideoUnderstanding", zodlite.Optional(zodlite.Boolean)),
	),
	"xai.image_generation": zodlite.Object(
		zodlite.F("action", zodlite.Optional(zodlite.Enum("auto", "generate", "edit"))),
	),
	"xai.file_search": zodlite.Object(
		zodlite.F("vectorStoreIds", zodlite.Array(zodlite.String)),
		zodlite.F("maxNumResults", zodlite.Optional(zodlite.Number)),
	),
	"xai.mcp": zodlite.Object(
		zodlite.F("serverUrl", zodlite.String),
		zodlite.F("serverLabel", zodlite.Optional(zodlite.String)),
		zodlite.F("serverDescription", zodlite.Optional(zodlite.String)),
		zodlite.F("allowedTools", zodlite.Optional(zodlite.Array(zodlite.String))),
		zodlite.F("headers", zodlite.Optional(zodlite.RecordOf(zodlite.String))),
		zodlite.F("authorization", zodlite.Optional(zodlite.String)),
	),
}

// validateXAIResponsesToolArgs mirrors the validateTypes call TS
// prepareResponsesTools makes on the args of the web search, X search, image
// generation, file search and MCP tools: invalid args throw TypeValidationError
// "Type validation failed: Value: <JSON.stringify(args)>.\nError message:
// <zod issues>" before the request is built.
func validateXAIResponsesToolArgs(tools []types.Tool) error {
	for _, t := range tools {
		id, isProvider := xaiProviderToolID(t)
		if !isProvider {
			continue
		}
		node, ok := xaiToolArgsNodes[id]
		if !ok {
			continue
		}
		data, err := jsonstr.Marshal(xaiToolArgs(t, id))
		if err != nil {
			return &providererrors.TypeValidationError{Context: &providererrors.ValidationContext{}, Message: err.Error(), Cause: err}
		}
		msg := zodlite.ValidationMessage(node, string(data))
		if msg == "" {
			continue
		}
		valueText := ""
		if _, after, ok := strings.Cut(msg, "Value: "); ok {
			valueText, _, _ = strings.Cut(after, ".\nError message: ")
		}
		return &providererrors.TypeValidationError{
			Context: &providererrors.ValidationContext{},
			Value:   valueText,
			Message: zodlite.IssueText(msg),
		}
	}
	return nil
}

// xaiToolArgs returns the TS args of an xAI provider tool: the typed Go
// config rendered as the constructor's args (the config is what the request
// sends), else the ProviderArgs of a TS-shaped tool.
func xaiToolArgs(t types.Tool, id string) map[string]interface{} {
	var built types.Tool
	switch cfg := t.ProviderOptions.(type) {
	case WebSearchConfig:
		built = WebSearch(cfg)
	case XSearchConfig:
		built = XSearch(cfg)
	case ImageGenerationConfig:
		built = ImageGeneration(cfg)
	case FileSearchOptions:
		built = FileSearch(FileSearchConfig(cfg))
	case FileSearchConfig:
		built = FileSearch(cfg)
	case MCPServerOptions:
		built = MCPServer(MCPServerConfig(cfg))
	case MCPServerConfig:
		built = MCPServer(cfg)
	default:
		if t.ProviderArgs == nil {
			return map[string]interface{}{}
		}
		return t.ProviderArgs
	}
	if built.ProviderID != id {
		// A typed config of another tool: validate the tool's own args.
		if t.ProviderArgs == nil {
			return map[string]interface{}{}
		}
		return t.ProviderArgs
	}
	return built.ProviderArgs
}

// xaiProviderToolID reports whether t is an xAI provider-defined tool and its
// id (TS tool.id). TS-shaped tools carry the id in ProviderID; Go-style tools
// use the canonical id as Name.
func xaiProviderToolID(t types.Tool) (string, bool) {
	switch {
	case t.ProviderID != "":
		return t.ProviderID, true
	case t.Type == "provider":
		return t.Name, true
	case strings.HasPrefix(t.Name, "xai."):
		return t.Name, true
	}
	return "", false
}

func argStrings(args map[string]interface{}, key string) []string {
	switch v := args[key].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func argString(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

func argBoolPtr(args map[string]interface{}, key string) *bool {
	if b, ok := args[key].(bool); ok {
		return &b
	}
	return nil
}

func argStringPtr(args map[string]interface{}, key string) *string {
	if s, ok := args[key].(string); ok {
		return &s
	}
	return nil
}

// The helpers below read a provider tool's typed ProviderOptions config, or
// the TS-style camelCase ProviderArgs of a TS-shaped tool.
func xaiWebSearchArgs(t types.Tool) WebSearchConfig {
	if cfg, ok := t.ProviderOptions.(WebSearchConfig); ok {
		return cfg
	}
	a := t.ProviderArgs
	return WebSearchConfig{
		AllowedDomains:           argStrings(a, "allowedDomains"),
		ExcludedDomains:          argStrings(a, "excludedDomains"),
		EnableImageSearch:        argBoolPtr(a, "enableImageSearch"),
		EnableImageUnderstanding: argBoolPtr(a, "enableImageUnderstanding"),
	}
}

func xaiXSearchArgs(t types.Tool) XSearchConfig {
	if cfg, ok := t.ProviderOptions.(XSearchConfig); ok {
		return cfg
	}
	a := t.ProviderArgs
	return XSearchConfig{
		AllowedXHandles:          argStrings(a, "allowedXHandles"),
		ExcludedXHandles:         argStrings(a, "excludedXHandles"),
		FromDate:                 argStringPtr(a, "fromDate"),
		ToDate:                   argStringPtr(a, "toDate"),
		EnableImageUnderstanding: argBoolPtr(a, "enableImageUnderstanding"),
		EnableVideoUnderstanding: argBoolPtr(a, "enableVideoUnderstanding"),
	}
}

func xaiImageGenerationArgs(t types.Tool) ImageGenerationConfig {
	if cfg, ok := t.ProviderOptions.(ImageGenerationConfig); ok {
		return cfg
	}
	return ImageGenerationConfig{Action: argString(t.ProviderArgs, "action")}
}

func xaiFileSearchArgs(t types.Tool) FileSearchOptions {
	if cfg, ok := t.ProviderOptions.(FileSearchOptions); ok {
		return cfg
	}
	if cfg, ok := t.ProviderOptions.(FileSearchConfig); ok {
		return FileSearchOptions(cfg)
	}
	out := FileSearchOptions{}
	if _, present := t.ProviderArgs["vectorStoreIds"]; present {
		out.VectorStoreIDs = append([]string{}, argStrings(t.ProviderArgs, "vectorStoreIds")...)
	}
	switch n := t.ProviderArgs["maxNumResults"].(type) {
	case int:
		out.MaxNumResults = n
	case float64:
		out.MaxNumResults = int(n)
	}
	return out
}

func xaiMCPArgs(t types.Tool) MCPServerOptions {
	if cfg, ok := t.ProviderOptions.(MCPServerOptions); ok {
		return cfg
	}
	a := t.ProviderArgs
	cfg := MCPServerOptions{
		ServerURL:         argString(a, "serverUrl"),
		ServerLabel:       argString(a, "serverLabel"),
		ServerDescription: argString(a, "serverDescription"),
		AllowedTools:      argStrings(a, "allowedTools"),
		Authorization:     argString(a, "authorization"),
	}
	switch h := a["headers"].(type) {
	case map[string]string:
		cfg.Headers = h
	case map[string]interface{}:
		cfg.Headers = map[string]string{}
		for k, v := range h {
			if str, ok := v.(string); ok {
				cfg.Headers[k] = str
			}
		}
	}
	return cfg
}

// prepareXAIResponsesTools converts SDK tools to XAI Responses API wire format.
// XAI provider tools are serialized using their ProviderOptions config.
// Regular function tools become {type:"function", name, description, parameters}.
func prepareXAIResponsesTools(tools []types.Tool) []interface{} {
	result, _ := prepareXAIResponsesToolsWithWarnings(tools)
	return result
}

// prepareXAIResponsesToolsWithWarnings is prepareXAIResponsesTools plus the TS
// toolWarnings: a provider-defined tool xAI does not know is dropped with an
// `unsupported` warning ("provider-defined tool <name>").
func prepareXAIResponsesToolsWithWarnings(tools []types.Tool) ([]interface{}, []types.Warning) {
	if len(tools) == 0 {
		return nil, nil
	}
	var warnings []types.Warning
	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		if id, isProvider := xaiProviderToolID(t); isProvider {
			if !xaiKnownProviderTools[id] {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "provider-defined tool " + t.Name,
				})
				continue
			}
			t.Name = id // convertXAIResponsesTool dispatches on the canonical id
		}
		result = append(result, convertXAIResponsesTool(t))
	}
	return result, warnings
}

var xaiKnownProviderTools = map[string]bool{
	"xai.web_search": true, "xai.x_search": true, "xai.code_execution": true,
	"xai.view_image": true, "xai.view_x_video": true, "xai.image_generation": true,
	"xai.file_search": true, "xai.mcp": true,
}

// resolveXAIResponsesToolChoice mirrors TS prepareResponsesTools tool choice:
// a choice naming an unknown tool is omitted; a choice naming a provider
// (server-side) tool is omitted with an unsupported warning because the xAI
// API cannot force server-side tools.
func resolveXAIResponsesToolChoice(tc types.ToolChoice, tools []types.Tool) (interface{}, []types.Warning) {
	if tc.Type != types.ToolChoiceTool {
		return convertXAIResponsesToolChoice(tc), nil
	}
	for _, t := range tools {
		if t.Name != tc.ToolName {
			continue
		}
		if _, isProvider := xaiProviderToolID(t); isProvider {
			return nil, []types.Warning{{
				Type:    "unsupported",
				Feature: fmt.Sprintf("toolChoice for server-side tool %q", t.Name),
			}}
		}
		return convertXAIResponsesToolChoice(tc), nil
	}
	return nil, nil
}

func convertXAIResponsesTool(t types.Tool) interface{} {
	switch t.Name {
	case "xai.web_search":
		cfg := xaiWebSearchArgs(t)
		m := map[string]interface{}{"type": "web_search"}
		if cfg.AllowedDomains != nil {
			m["allowed_domains"] = cfg.AllowedDomains
		}
		if cfg.ExcludedDomains != nil {
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
		cfg := xaiXSearchArgs(t)
		m := map[string]interface{}{"type": "x_search"}
		if cfg.AllowedXHandles != nil {
			m["allowed_x_handles"] = cfg.AllowedXHandles
		}
		if cfg.ExcludedXHandles != nil {
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
		cfg := xaiFileSearchArgs(t)
		m := map[string]interface{}{"type": "file_search"}
		if cfg.VectorStoreIDs != nil {
			m["vector_store_ids"] = cfg.VectorStoreIDs
		}
		if cfg.MaxNumResults > 0 {
			m["max_num_results"] = cfg.MaxNumResults
		}
		return m

	case "xai.image_generation":
		cfg := xaiImageGenerationArgs(t)
		m := map[string]interface{}{"type": "image_generation"}
		if cfg.Action != "" {
			m["action"] = cfg.Action
		}
		return m

	case "xai.mcp":
		cfg := xaiMCPArgs(t)
		m := map[string]interface{}{"type": "mcp", "server_url": cfg.ServerURL}
		if cfg.ServerLabel != "" {
			m["server_label"] = cfg.ServerLabel
		}
		if cfg.ServerDescription != "" {
			m["server_description"] = cfg.ServerDescription
		}
		if cfg.AllowedTools != nil {
			m["allowed_tools"] = cfg.AllowedTools
		}
		if cfg.Headers != nil {
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
