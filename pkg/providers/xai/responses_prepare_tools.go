package xai

import (
	"fmt"
	"strings"

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
		id, isProvider := xaiProviderToolID(t)
		if !isProvider {
			continue
		}
		switch id {
		case "xai.web_search":
			cfg := xaiWebSearchArgs(t)
			if len(cfg.AllowedDomains) > 5 {
				return tooBig(t.Name, "allowedDomains", 5)
			}
			if len(cfg.ExcludedDomains) > 5 {
				return tooBig(t.Name, "excludedDomains", 5)
			}
		case "xai.x_search":
			cfg := xaiXSearchArgs(t)
			if len(cfg.AllowedXHandles) > 20 {
				return tooBig(t.Name, "allowedXHandles", 20)
			}
			if len(cfg.ExcludedXHandles) > 20 {
				return tooBig(t.Name, "excludedXHandles", 20)
			}
		case "xai.image_generation":
			cfg := xaiImageGenerationArgs(t)
			switch cfg.Action {
			case "", "auto", "generate", "edit":
			default:
				return providererrors.NewValidationError("tools",
					fmt.Sprintf("Type validation failed for %s: action: Invalid option: expected one of \"auto\"|\"generate\"|\"edit\"", t.Name), nil)
			}
		case "xai.file_search":
			// fileSearchArgsSchema: vectorStoreIds is a required array of
			// strings (an empty array is valid); maxNumResults an optional
			// number.
			if msg := validateXAIArgs(t, []xaiArgRule{
				{key: "vectorStoreIds", kind: xaiArgStringArray, required: true},
				{key: "maxNumResults", kind: xaiArgNumber},
			}); msg != "" {
				return providererrors.NewValidationError("tools",
					fmt.Sprintf("Type validation failed for %s: %s", t.Name, msg), nil)
			}
			if _, typed := xaiTypedFileSearch(t); typed && xaiFileSearchArgs(t).VectorStoreIDs == nil {
				return providererrors.NewValidationError("tools",
					fmt.Sprintf("Type validation failed for %s: vectorStoreIds: Invalid input: expected array, received undefined", t.Name), nil)
			}
		case "xai.mcp":
			// mcpServerArgsSchema: serverUrl is a required string; the other
			// fields are optional strings, a string array and a string
			// record.
			if msg := validateXAIArgs(t, []xaiArgRule{
				{key: "serverUrl", kind: xaiArgString, required: true},
				{key: "serverLabel", kind: xaiArgString},
				{key: "serverDescription", kind: xaiArgString},
				{key: "allowedTools", kind: xaiArgStringArray},
				{key: "headers", kind: xaiArgStringRecord},
				{key: "authorization", kind: xaiArgString},
			}); msg != "" {
				return providererrors.NewValidationError("tools",
					fmt.Sprintf("Type validation failed for %s: %s", t.Name, msg), nil)
			}
		}
	}
	return nil
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

type xaiArgKind int

const (
	xaiArgString xaiArgKind = iota
	xaiArgNumber
	xaiArgStringArray
	xaiArgStringRecord
)

type xaiArgRule struct {
	key      string
	kind     xaiArgKind
	required bool
}

// xaiTypedFileSearch reports whether the tool carries a typed Go config.
func xaiTypedFileSearch(t types.Tool) (FileSearchOptions, bool) {
	switch cfg := t.ProviderOptions.(type) {
	case FileSearchOptions:
		return cfg, true
	case FileSearchConfig:
		return FileSearchOptions(cfg), true
	}
	return FileSearchOptions{}, false
}

// validateXAIArgs applies zod-style type checks to the TS-shaped ProviderArgs
// of a provider tool (typed Go configs are already well-typed and skip this).
// It returns the first failure as `<path>: Invalid input: expected X,
// received Y`, or "" when the args are valid.
func validateXAIArgs(t types.Tool, rules []xaiArgRule) string {
	switch t.ProviderOptions.(type) {
	case FileSearchOptions, FileSearchConfig, MCPServerOptions, MCPServerConfig:
		return ""
	}
	for _, r := range rules {
		v, present := t.ProviderArgs[r.key]
		if !present || v == nil {
			if r.required {
				want := "string"
				switch r.kind {
				case xaiArgStringArray:
					want = "array"
				case xaiArgNumber:
					want = "number"
				case xaiArgStringRecord:
					want = "record"
				}
				recv := "undefined"
				if present {
					recv = "null"
				}
				return fmt.Sprintf("%s: Invalid input: expected %s, received %s", r.key, want, recv)
			}
			if present {
				return fmt.Sprintf("%s: Invalid input: received null", r.key)
			}
			continue
		}
		switch r.kind {
		case xaiArgString:
			if _, ok := v.(string); !ok {
				return fmt.Sprintf("%s: Invalid input: expected string, received %s", r.key, xaiJSType(v))
			}
		case xaiArgNumber:
			switch v.(type) {
			case int, int32, int64, float32, float64:
			default:
				return fmt.Sprintf("%s: Invalid input: expected number, received %s", r.key, xaiJSType(v))
			}
		case xaiArgStringArray:
			switch arr := v.(type) {
			case []string:
			case []interface{}:
				for i, e := range arr {
					if _, ok := e.(string); !ok {
						return fmt.Sprintf("%s[%d]: Invalid input: expected string, received %s", r.key, i, xaiJSType(e))
					}
				}
			default:
				return fmt.Sprintf("%s: Invalid input: expected array, received %s", r.key, xaiJSType(v))
			}
		case xaiArgStringRecord:
			switch m := v.(type) {
			case map[string]string:
			case map[string]interface{}:
				for k, e := range m {
					if _, ok := e.(string); !ok {
						return fmt.Sprintf("%s.%s: Invalid input: expected string, received %s", r.key, k, xaiJSType(e))
					}
				}
			default:
				return fmt.Sprintf("%s: Invalid input: expected record, received %s", r.key, xaiJSType(v))
			}
		}
	}
	return ""
}

// xaiJSType names a Go value the way zod names the JS value it would be.
func xaiJSType(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int32, int64, float32, float64:
		return "number"
	case []interface{}, []string:
		return "array"
	case map[string]interface{}, map[string]string:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
