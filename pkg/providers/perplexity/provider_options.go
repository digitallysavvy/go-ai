package perplexity

import (
	"fmt"
	"math"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file mirrors TS perplexity-language-model-options.ts: the Agent API's
// providerOptions.perplexity schema. It is a wholesale replacement of the
// pre-migration Sonar Chat Completions options (search_recency_filter,
// web_search_options, etc.) -- none of the old option names are aliased in
// the new API.

// perplexityLanguageModelOptions mirrors PerplexityLanguageModelOptions (the
// zod-inferred type of perplexityLanguageModelOptions in TS). It is a "loose
// object": every documented field is validated when present, and unrecognized
// top-level keys are preserved for passthrough (see
// parsePerplexityAgentOptions's extras return value).
type perplexityLanguageModelOptions struct {
	// Instructions are top-level Agent API instructions.
	Instructions string

	// Tools are native Agent API tools (web_search, fetch_url, people_search,
	// finance_search, sandbox, mcp, connector). AI SDK function tools are
	// supplied separately via the top-level `tools` call option.
	Tools []map[string]interface{}

	// Models is a fallback model list for Agent API routing.
	Models []string

	// MaxSteps is the maximum number of agentic steps. Must be a positive integer.
	MaxSteps *int64

	// MaxToolCalls is the maximum number of native tool calls. Must be a
	// nonnegative integer.
	MaxToolCalls *int64

	// PreviousResponseID continues a conversation from an earlier Agent API response.
	PreviousResponseID string

	// Store controls whether the response can be retrieved later.
	Store *bool

	// LanguagePreference is the preferred response language (ISO 639-1).
	LanguagePreference string

	// Reasoning is the Agent API reasoning configuration
	// ({"effort": "minimal"|"low"|"medium"|"high"|"xhigh"}), kept as a loose
	// map so unrecognized nested keys pass through unchanged.
	Reasoning map[string]interface{}

	// Skills is the Agent API skill configuration.
	Skills []map[string]interface{}
}

var perplexityAgentKnownOptionKeys = map[string]bool{
	"instructions":         true,
	"tools":                true,
	"models":               true,
	"max_steps":            true,
	"max_tool_calls":       true,
	"previous_response_id": true,
	"store":                true,
	"language_preference":  true,
	"reasoning":            true,
	"skills":               true,
}

var perplexityAgentReasoningEfforts = map[string]bool{
	"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true,
}

func invalidPerplexityProviderOptions(field, message string) error {
	return &providererrors.InvalidArgumentError{
		Field:   field,
		Message: message,
	}
}

// parsePerplexityAgentOptions parses+validates the resolved
// providerOptions.perplexity map into a typed struct, mirroring TS
// parseProviderOptions with the perplexityLanguageModelOptions zod schema.
// Because the TS schema is a "loose object", the second return value carries
// any top-level keys not modeled by perplexityLanguageModelOptions so they
// can still be forwarded verbatim (e.g. a future Agent API option).
func parsePerplexityAgentOptions(raw map[string]interface{}) (*perplexityLanguageModelOptions, map[string]interface{}, error) {
	if raw == nil {
		return nil, nil, nil
	}

	opts := &perplexityLanguageModelOptions{}
	var err error

	if opts.Instructions, err = perplexityAgentStringField(raw, "instructions"); err != nil {
		return nil, nil, err
	}
	if opts.Tools, err = perplexityAgentObjectArrayField(raw, "tools"); err != nil {
		return nil, nil, err
	}
	for _, tool := range opts.Tools {
		if err := validatePerplexityNativeTool(tool); err != nil {
			return nil, nil, err
		}
	}
	if opts.Models, err = perplexityAgentStringArrayField(raw, "models"); err != nil {
		return nil, nil, err
	}
	if opts.MaxSteps, err = perplexityAgentPositiveIntField(raw, "max_steps"); err != nil {
		return nil, nil, err
	}
	if opts.MaxToolCalls, err = perplexityAgentNonnegativeIntField(raw, "max_tool_calls"); err != nil {
		return nil, nil, err
	}
	if opts.PreviousResponseID, err = perplexityAgentStringField(raw, "previous_response_id"); err != nil {
		return nil, nil, err
	}
	if opts.Store, err = perplexityAgentBoolField(raw, "store"); err != nil {
		return nil, nil, err
	}
	if opts.LanguagePreference, err = perplexityAgentStringField(raw, "language_preference"); err != nil {
		return nil, nil, err
	}
	if opts.Reasoning, err = perplexityAgentReasoningField(raw); err != nil {
		return nil, nil, err
	}
	if opts.Skills, err = perplexityAgentObjectArrayField(raw, "skills"); err != nil {
		return nil, nil, err
	}

	var extras map[string]interface{}
	for k, v := range raw {
		if !perplexityAgentKnownOptionKeys[k] {
			if extras == nil {
				extras = map[string]interface{}{}
			}
			extras[k] = v
		}
	}

	return opts, extras, nil
}

func perplexityAgentStringField(raw map[string]interface{}, field string) (string, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a string, got %T", v))
	}
	return s, nil
}

func perplexityAgentBoolField(raw map[string]interface{}, field string) (*bool, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a boolean, got %T", v))
	}
	return &b, nil
}

func perplexityAgentStringArrayField(raw map[string]interface{}, field string) ([]string, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be an array of strings, got %T", v))
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, invalidPerplexityProviderOptions(field, "must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func perplexityAgentObjectArrayField(raw map[string]interface{}, field string) ([]map[string]interface{}, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be an array, got %T", v))
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, invalidPerplexityProviderOptions(field, "must be an array of objects")
		}
		out = append(out, m)
	}
	return out, nil
}

func perplexityAgentPositiveIntField(raw map[string]interface{}, field string) (*int64, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f <= 0 {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a positive integer, got %v", v))
	}
	n := int64(f)
	return &n, nil
}

func perplexityAgentNonnegativeIntField(raw map[string]interface{}, field string) (*int64, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 0 {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a nonnegative integer, got %v", v))
	}
	n := int64(f)
	return &n, nil
}

// perplexityAgentReasoningField validates providerOptions.perplexity.reasoning,
// mirroring TS's z.looseObject({ effort: z.enum([...]).optional() }): only
// "effort" is checked, and any other nested key is preserved as-is.
func perplexityAgentReasoningField(raw map[string]interface{}) (map[string]interface{}, error) {
	v, ok := raw["reasoning"]
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, invalidPerplexityProviderOptions("reasoning", fmt.Sprintf("must be an object, got %T", v))
	}
	if effort, ok := m["effort"]; ok && effort != nil {
		s, ok := effort.(string)
		if !ok || !perplexityAgentReasoningEfforts[s] {
			return nil, invalidPerplexityProviderOptions("reasoning.effort", fmt.Sprintf("invalid value %v", effort))
		}
	}
	return m, nil
}

var perplexityWebSearchRecencyFilters = map[string]bool{
	"hour": true, "day": true, "week": true, "month": true, "year": true,
}

var perplexitySearchContextSizes = map[string]bool{
	"low": true, "medium": true, "high": true,
}

var perplexityKnownNativeToolTypes = map[string]bool{
	"web_search": true, "fetch_url": true, "people_search": true,
	"finance_search": true, "sandbox": true, "mcp": true, "connector": true,
}

// validatePerplexityNativeTool mirrors TS's nativeToolSchema union: it checks
// the required fields and enum values for the recognized native tool types
// (web_search/fetch_url/people_search/finance_search/sandbox/mcp/connector).
// Types outside this set are TS-only in the sense that the SDK's own schema
// doesn't recognize them either (nativeToolSchema is a closed z.union, not a
// looseObject) -- but since providerOptions.perplexity itself is a
// z.looseObject and future Agent API tool types are expected, an unrecognized
// "type" is passed through unchecked here rather than rejected, matching the
// forward-compatible passthrough documented for the rest of this options
// object (see parsePerplexityAgentOptions's extras).
func validatePerplexityNativeTool(tool map[string]interface{}) error {
	toolType, _ := tool["type"].(string)
	if toolType == "" {
		return invalidPerplexityProviderOptions("tools", "each tool must have a string \"type\"")
	}
	if !perplexityKnownNativeToolTypes[toolType] {
		return nil
	}

	switch toolType {
	case "mcp":
		if err := requirePerplexityStringField(tool, "tools", "mcp.server_label"); err != nil {
			return err
		}
		if err := requirePerplexityStringField(tool, "tools", "mcp.server_url"); err != nil {
			return err
		}
	case "connector":
		if err := requirePerplexityStringField(tool, "tools", "connector.id"); err != nil {
			return err
		}
		if err := requirePerplexityStringField(tool, "tools", "connector.server_label"); err != nil {
			return err
		}
	case "web_search":
		if filters, ok := tool["filters"].(map[string]interface{}); ok {
			if recency, ok := filters["search_recency_filter"]; ok && recency != nil {
				s, ok := recency.(string)
				if !ok || !perplexityWebSearchRecencyFilters[s] {
					return invalidPerplexityProviderOptions("tools", fmt.Sprintf("web_search.filters.search_recency_filter: invalid value %v", recency))
				}
			}
		}
		if size, ok := tool["search_context_size"]; ok && size != nil {
			s, ok := size.(string)
			if !ok || !perplexitySearchContextSizes[s] {
				return invalidPerplexityProviderOptions("tools", fmt.Sprintf("web_search.search_context_size: invalid value %v", size))
			}
		}
	}
	return nil
}

func requirePerplexityStringField(tool map[string]interface{}, field, path string) error {
	v, ok := tool[fieldKeyFromPath(path)]
	if !ok {
		return invalidPerplexityProviderOptions(field, fmt.Sprintf("%s is required", path))
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return invalidPerplexityProviderOptions(field, fmt.Sprintf("%s must be a non-empty string", path))
	}
	return nil
}

// fieldKeyFromPath extracts the wire field name from a "type.field" path used
// for error messages (e.g. "mcp.server_label" -> "server_label").
func fieldKeyFromPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i+1:]
		}
	}
	return path
}

// toBodyMap serializes the validated agent options (minus Tools, which the
// caller merges separately alongside AI SDK function tools) into a flat map
// of wire keys, ready to be spread into the /v1/agent request body --
// mirrors the TS SDK's `...agentOptions` spread.
func (o *perplexityLanguageModelOptions) toBodyMap() map[string]interface{} {
	if o == nil {
		return nil
	}
	body := map[string]interface{}{}
	if o.Instructions != "" {
		body["instructions"] = o.Instructions
	}
	if o.Models != nil {
		body["models"] = o.Models
	}
	if o.MaxSteps != nil {
		body["max_steps"] = *o.MaxSteps
	}
	if o.MaxToolCalls != nil {
		body["max_tool_calls"] = *o.MaxToolCalls
	}
	if o.PreviousResponseID != "" {
		body["previous_response_id"] = o.PreviousResponseID
	}
	if o.Store != nil {
		body["store"] = *o.Store
	}
	if o.LanguagePreference != "" {
		body["language_preference"] = o.LanguagePreference
	}
	if o.Skills != nil {
		body["skills"] = o.Skills
	}
	return body
}
