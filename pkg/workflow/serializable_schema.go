package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// MarshalSchema converts any JSON-serializable schema into a map.
func MarshalSchema(schema interface{}) (map[string]interface{}, error) {
	if schema == nil {
		return nil, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnmarshalSchema reconstructs schema as generic JSON value.
func UnmarshalSchema(data map[string]interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SerializableToolDef is the JSON-safe representation of a workflow tool.
// Function fields such as Execute, approval predicates, and result mappers are
// intentionally omitted because workflow state must cross process boundaries.
type SerializableToolDef struct {
	Name                    string                   `json:"name"`
	Description             string                   `json:"description,omitempty"`
	Title                   string                   `json:"title,omitempty"`
	Parameters              map[string]interface{}   `json:"parameters,omitempty"`
	InputSchema             map[string]interface{}   `json:"inputSchema,omitempty"`
	Type                    string                   `json:"type,omitempty"`
	ID                      string                   `json:"id,omitempty"`
	Args                    map[string]interface{}   `json:"args,omitempty"`
	Strict                  *bool                    `json:"strict,omitempty"`
	ProviderExecuted        bool                     `json:"providerExecuted,omitempty"`
	IsProviderExecuted      bool                     `json:"isProviderExecuted,omitempty"`
	SupportsDeferredResults bool                     `json:"supportsDeferredResults,omitempty"`
	InputExamples           []types.ToolInputExample `json:"inputExamples,omitempty"`
	ProviderMetadata        map[string]interface{}   `json:"providerMetadata,omitempty"`
	// HasOwnValidator records that the original input schema validates (and
	// possibly normalizes) input with its own runtime validator, not just JSON
	// Schema. ValidateSerializableToolInput defers to that original validator
	// instead of re-validating the lossy JSON Schema copy (TS #22397).
	HasOwnValidator bool `json:"hasOwnValidator,omitempty"`
}

// SerializeToolSet converts tools to a JSON-safe map keyed by tool name.
func SerializeToolSet(tools []types.Tool) (map[string]SerializableToolDef, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make(map[string]SerializableToolDef, len(tools))
	for _, tool := range tools {
		params, hasOwnValidator, err := serializeToolParameters(tool.Parameters)
		if err != nil {
			return nil, err
		}
		def := SerializableToolDef{
			Name:                    tool.Name,
			Description:             tool.Description,
			Title:                   tool.Title,
			Parameters:              params,
			InputSchema:             params,
			Strict:                  tool.Strict,
			ProviderExecuted:        tool.ProviderExecuted,
			IsProviderExecuted:      tool.ProviderExecuted,
			SupportsDeferredResults: tool.SupportsDeferredResults,
			InputExamples:           tool.InputExamples,
			ProviderMetadata:        tool.ProviderMetadata,
			HasOwnValidator:         hasOwnValidator,
		}
		if tool.ProviderExecuted {
			def.Type = "provider"
		}
		if providerArgs, err := MarshalSchema(tool.ProviderOptions); err == nil {
			def.Args = providerArgs
			if id, ok := providerArgs["id"].(string); ok {
				def.ID = id
			}
			if typ, ok := providerArgs["type"].(string); ok {
				def.Type = typ
			}
		}
		out[tool.Name] = def
	}
	return out, nil
}

// serializeToolParameters converts a tool's Parameters to a JSON Schema map.
// A schema.Schema contributes its validator's JSON Schema, and reports
// hasOwnValidator when that validator is anything other than a plain JSON
// Schema validator (for example a struct-based validator).
func serializeToolParameters(parameters interface{}) (map[string]interface{}, bool, error) {
	if s, ok := parameters.(schema.Schema); ok && s != nil {
		if v := s.Validator(); v != nil {
			params, err := MarshalSchema(v.JSONSchema())
			_, plainJSON := v.(*schema.JSONSchemaValidator)
			return params, !plainJSON, err
		}
	}
	params, err := MarshalSchema(parameters)
	return params, false, err
}

// ResolveSerializableTools reconstructs non-executable tool descriptors from
// serialized definitions. Callers can attach Execute functions by name after
// deserialization.
func ResolveSerializableTools(defs map[string]SerializableToolDef) []types.Tool {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]types.Tool, 0, len(defs))
	for name, def := range defs {
		toolName := def.Name
		if toolName == "" {
			toolName = name
		}
		params := def.Parameters
		if params == nil {
			params = def.InputSchema
		}
		tools = append(tools, types.Tool{
			Name:                    toolName,
			Description:             def.Description,
			Title:                   def.Title,
			Parameters:              params,
			Strict:                  def.Strict,
			ProviderExecuted:        def.ProviderExecuted || def.IsProviderExecuted || def.Type == "provider",
			SupportsDeferredResults: def.SupportsDeferredResults,
			InputExamples:           def.InputExamples,
			ProviderOptions:         def.Args,
			ProviderMetadata:        def.ProviderMetadata,
		})
	}
	return tools
}

// ValidateSerializableToolInput validates a tool input against its serialized
// JSON Schema and returns the defaulted value. Defaults declared on the
// schema are applied before validating (matching
// pkg/ai/tool_call_pipeline.go's applyToolCallInputDefaults and the rest of
// the codebase's apply-defaults-before-validate convention): a field missing
// from input but declared with a schema default must not fail validation.
// Like tool_call_pipeline.go's args and validate_ui_messages.go's parsed
// value, the returned value -- not the raw input -- is what a caller should
// go on to pass to the resolved tool's Execute, so the schema's defaults
// actually reach the tool (mirrors TS's validateTypes/safeValidateTypes,
// whose defaulted parse result is always what downstream code consumes).
func ValidateSerializableToolInput(def SerializableToolDef, input interface{}) (interface{}, error) {
	if def.HasOwnValidator {
		// The original schema validates after the durable boundary, where it
		// is available; the JSON Schema copy here cannot reproduce it.
		return input, nil
	}
	params := def.InputSchema
	if params == nil {
		params = def.Parameters
	}
	if params == nil {
		return input, nil
	}
	// Formats are annotations here, not assertions, and unknown keywords and
	// formats never reject a schema (TS #22080 validates with Ajv
	// validateFormats: false).
	normalized, err := MarshalSchema(params) // normalizes []string etc. to JSON shapes
	if err != nil {
		return nil, fmt.Errorf("workflow: invalid schema for %s: %w", def.Name, err)
	}
	params = stripSchemaFormats(normalized).(map[string]interface{})
	s := schema.NewSimpleJSONSchema(params)
	defaulted := schema.ApplyDefaults(input, s)
	if err := s.Validator().Validate(defaulted); err != nil {
		name := def.Name
		if name == "" {
			name = "tool"
		}
		if details := collectActionableErrors(defaulted, params, "data", nil); len(details) > 0 {
			return nil, fmt.Errorf("workflow: invalid input for %s: %s", name, strings.Join(details, ", "))
		}
		return nil, fmt.Errorf("workflow: invalid input for %s: %w", name, err)
	}
	return defaulted, nil
}

// stripSchemaFormats returns a deep copy of a JSON Schema value with string
// "format" keywords removed.
func stripSchemaFormats(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if k == "format" {
				if _, isString := val.(string); isString {
					continue
				}
			}
			out[k] = stripSchemaFormats(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = stripSchemaFormats(val)
		}
		return out
	default:
		return v
	}
}

// collectActionableErrors walks value against sch and reports every enum,
// const, required and additionalProperties violation with the details a model
// needs to correct the call (allowed values, unexpected property names, and
// every invalid array location). Messages mirror TS formatAjvError (#22392).
// It covers properties, items and allOf; other keywords are left to the
// generic validator error.
func collectActionableErrors(value interface{}, sch map[string]interface{}, path string, out []string) []string {
	if sch == nil {
		return out
	}
	if enum, ok := sch["enum"].([]interface{}); ok && len(enum) > 0 {
		matched := false
		for _, e := range enum {
			if jsonEqual(value, e) {
				matched = true
				break
			}
		}
		if !matched {
			parts := make([]string, len(enum))
			for i, e := range enum {
				parts[i] = jsonString(e)
			}
			out = append(out, fmt.Sprintf("%s must be one of %s", path, strings.Join(parts, ", ")))
		}
	}
	if c, ok := sch["const"]; ok && !jsonEqual(value, c) {
		out = append(out, fmt.Sprintf("%s must equal %s", path, jsonString(c)))
	}
	if obj, ok := value.(map[string]interface{}); ok {
		if req, ok := sch["required"].([]interface{}); ok {
			for _, r := range req {
				if name, ok := r.(string); ok {
					if _, exists := obj[name]; !exists {
						out = append(out, fmt.Sprintf("%s is missing required property %s", path, jsonString(name)))
					}
				}
			}
		}
		props, _ := sch["properties"].(map[string]interface{})
		if add, ok := sch["additionalProperties"].(bool); ok && !add {
			keys := make([]string, 0, len(obj))
			for k := range obj {
				if _, declared := props[k]; !declared {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				out = append(out, fmt.Sprintf("%s has unexpected property %s", path, jsonString(k)))
			}
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child, exists := obj[k]
			sub, ok := props[k].(map[string]interface{})
			if exists && ok {
				out = collectActionableErrors(child, sub, path+"/"+k, out)
			}
		}
	}
	if arr, ok := value.([]interface{}); ok {
		if items, ok := sch["items"].(map[string]interface{}); ok {
			for i, el := range arr {
				out = collectActionableErrors(el, items, fmt.Sprintf("%s/%d", path, i), out)
			}
		}
	}
	if allOf, ok := sch["allOf"].([]interface{}); ok {
		for _, sub := range allOf {
			if m, ok := sub.(map[string]interface{}); ok {
				out = collectActionableErrors(value, m, path, out)
			}
		}
	}
	return out
}

func jsonString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func jsonEqual(a, b interface{}) bool {
	return jsonString(a) == jsonString(b)
}
