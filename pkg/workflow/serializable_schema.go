package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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

	// Metadata, ProviderOptions and the HasOnInput* flags mirror the TS
	// SerializableToolDef fields of the same names. Callbacks themselves never
	// cross the boundary; the flags tell the receiver that the original tool
	// has them.
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
	HasOnInputStart bool                   `json:"hasOnInputStart,omitempty"`
	HasOnInputDelta bool                   `json:"hasOnInputDelta,omitempty"`
	HasOnInputAvail bool                   `json:"hasOnInputAvailable,omitempty"`

	// parametersHint and inputSchemaHint hold the key order of the schema the
	// definition was made from (or decoded from), which the map fields cannot
	// keep. They survive MarshalJSON/UnmarshalJSON and ResolveSerializableTools,
	// so an MCP tool's schema keeps the server's key order across a durable
	// boundary, like TS (JSON text keeps its order).
	parametersHint  interface{}
	inputSchemaHint interface{}
}

// serializableToolDefJSON has the fields of SerializableToolDef and no methods,
// so MarshalJSON and UnmarshalJSON can reuse the struct's own encoding.
type serializableToolDefJSON SerializableToolDef

// MarshalJSON writes the definition with its schemas in their source key order.
func (d SerializableToolDef) MarshalJSON() ([]byte, error) {
	if d.parametersHint == nil && d.inputSchemaHint == nil {
		return json.Marshal(serializableToolDefJSON(d))
	}
	aux := struct {
		serializableToolDefJSON
		Parameters  json.RawMessage `json:"parameters,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	}{serializableToolDefJSON: serializableToolDefJSON(d)}
	var err error
	if aux.Parameters, err = marshalOrdered(d.Parameters, d.parametersHint); err != nil {
		return nil, err
	}
	if aux.InputSchema, err = marshalOrdered(d.InputSchema, d.inputSchemaHint); err != nil {
		return nil, err
	}
	return json.Marshal(aux)
}

func marshalOrdered(m map[string]interface{}, hint interface{}) (json.RawMessage, error) {
	if m == nil {
		return nil, nil
	}
	if hint == nil {
		return json.Marshal(m)
	}
	return jsonorder.Hinted{V: m, Hint: hint}.MarshalJSON()
}

// UnmarshalJSON reads the definition and remembers the key order of its
// schemas.
func (d *SerializableToolDef) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, (*serializableToolDefJSON)(d)); err != nil {
		return err
	}
	var raw struct {
		Parameters  json.RawMessage `json:"parameters"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	d.parametersHint, d.inputSchemaHint = nil, nil
	if json.Unmarshal(data, &raw) == nil {
		d.parametersHint = parseOrderHint(raw.Parameters)
		d.inputSchemaHint = parseOrderHint(raw.InputSchema)
	}
	return nil
}

func parseOrderHint(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	if v, ok := jsonorder.Parse(raw); ok {
		if o, isObj := v.(*jsonorder.Object); isObj {
			return o
		}
	}
	return nil
}

// toolParametersHint is the key order of a tool's Parameters when it carries
// one (jsonorder.Hinted from an MCP server, or an *schema.OrderedSchema).
func toolParametersHint(tool types.Tool) interface{} {
	if o, ok := tool.ParameterKeyOrder.(*jsonorder.Object); ok && o != nil {
		if _, isMap := tool.Parameters.(map[string]interface{}); isMap {
			return o
		}
	}
	var raw []byte
	switch p := tool.Parameters.(type) {
	case jsonorder.Hinted:
		raw, _ = p.MarshalJSON() //nolint:errcheck // no hint on failure
	case *jsonorder.Hinted:
		if p != nil {
			raw, _ = p.MarshalJSON() //nolint:errcheck // no hint on failure
		}
	case *schema.OrderedSchema:
		if p != nil {
			raw, _ = p.MarshalJSON() //nolint:errcheck // no hint on failure
		}
	}
	return parseOrderHint(raw)
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
		hint := toolParametersHint(tool)
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
			parametersHint:          hint,
			inputSchemaHint:         hint,
		}
		if tool.ProviderExecuted {
			def.Type = "provider"
		}
		def.Metadata = tool.Metadata
		def.HasOnInputStart = tool.OnInputStart != nil
		def.HasOnInputDelta = tool.OnInputDelta != nil
		def.HasOnInputAvail = tool.OnInputAvailable != nil
		providerOptions, err := MarshalSchema(tool.ProviderOptions)
		if err != nil {
			providerOptions = nil
		}
		def.ProviderOptions = providerOptions
		switch tool.Type {
		case "dynamic":
			def.Type = "dynamic"
		case "provider":
			// Keep the provider tool identity so the receiver can tell it is
			// a provider-defined tool (TS: type, id, args).
			def.Type = "provider"
			def.ID = tool.ProviderID
			def.Args = tool.ProviderArgs
		default:
			// Before ProviderOptions had its own field it travelled in Args.
			def.Args = providerOptions
			if id, ok := providerOptions["id"].(string); ok {
				def.ID = id
			}
			if typ, ok := providerOptions["type"].(string); ok {
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
		if o, isOrdered := s.(*schema.OrderedSchema); isOrdered {
			// Plain JSON Schema that only keeps its key order: no own validator.
			params, err := MarshalSchema(o)
			return params, false, err
		}
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
	// A Go map has no order; TS keeps the record's insertion order. Sorting by
	// key gives a stable tool order (it is part of the request, so it keeps
	// prompt caches warm), like orderedTools does for a ToolSet.
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]types.Tool, 0, len(defs))
	for _, name := range names {
		def := defs[name]
		toolName := def.Name
		if toolName == "" {
			toolName = name
		}
		params := def.Parameters
		hint := def.parametersHint
		if params == nil {
			params = def.InputSchema
			hint = def.inputSchemaHint
		}
		// Parameters stays a plain map (as in v0.5.2); the key order rides
		// in ParameterKeyOrder.
		var parameters interface{} = params
		var keyOrder interface{}
		if hint != nil && params != nil {
			keyOrder = hint
		}
		providerOptions := def.ProviderOptions
		resolved := types.Tool{
			Name:                    toolName,
			Description:             def.Description,
			Title:                   def.Title,
			Parameters:              parameters,
			ParameterKeyOrder:       keyOrder,
			Strict:                  def.Strict,
			ProviderExecuted:        def.ProviderExecuted || def.IsProviderExecuted || def.Type == "provider",
			SupportsDeferredResults: def.SupportsDeferredResults,
			InputExamples:           def.InputExamples,
			ProviderMetadata:        def.ProviderMetadata,
			Metadata:                def.Metadata,
		}
		switch {
		case def.Type == "provider" && def.ID != "":
			resolved.Type = "provider"
			resolved.ProviderID = def.ID
			resolved.ProviderArgs = def.Args
			if providerOptions != nil {
				resolved.ProviderOptions = providerOptions
			}
		case def.Type == "dynamic":
			resolved.Type = "dynamic"
			resolved.ProviderOptions = optionsOrArgs(providerOptions, def.Args)
		default:
			resolved.ProviderOptions = optionsOrArgs(providerOptions, def.Args)
		}
		tools = append(tools, resolved)
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
	out = appendTypeAndBoundErrors(value, sch, path, out)
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
				out = collectActionableErrors(child, sub, path+"/"+escapeInstancePathKey(k), out)
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

// escapeInstancePathKey escapes a property name for a JSON pointer, as Ajv's
// instancePath does.
func escapeInstancePathKey(k string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(k)
}

func jsonNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func matchesJSONType(v interface{}, name string) bool {
	f, isNum := jsonNumber(v)
	switch name {
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		return isNum
	case "integer":
		return isNum && f == math.Trunc(f) && !math.IsInf(f, 0)
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "object":
		_, ok := v.(map[string]interface{})
		return ok
	case "array":
		_, ok := v.([]interface{})
		return ok
	}
	return true
}

func formatJSNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// appendTypeAndBoundErrors adds the Ajv messages for the type keyword and for
// the numeric, string and array bounds that apply to the value's own type
// (TS formatAjvError falls through to `data<path> <ajv message>` for them).
func appendTypeAndBoundErrors(value interface{}, sch map[string]interface{}, path string, out []string) []string {
	var names []string
	switch t := sch["type"].(type) {
	case string:
		names = []string{t}
	case []interface{}:
		for _, e := range t {
			if n, ok := e.(string); ok {
				names = append(names, n)
			}
		}
	case []string:
		names = t
	}
	if len(names) > 0 {
		matched := false
		for _, n := range names {
			if matchesJSONType(value, n) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, fmt.Sprintf("%s must be %s", path, strings.Join(names, ",")))
		}
	}
	bound := func(key string) (float64, bool) {
		v, ok := sch[key]
		if !ok {
			return 0, false
		}
		return jsonNumber(v)
	}
	if f, ok := jsonNumber(value); ok {
		if m, has := bound("multipleOf"); has && m != 0 {
			if q := f / m; q != math.Trunc(q) {
				out = append(out, fmt.Sprintf("%s must be multiple of %s", path, formatJSNumber(m)))
			}
		}
		if m, has := bound("maximum"); has && f > m {
			out = append(out, fmt.Sprintf("%s must be <= %s", path, formatJSNumber(m)))
		}
		if m, has := bound("minimum"); has && f < m {
			out = append(out, fmt.Sprintf("%s must be >= %s", path, formatJSNumber(m)))
		}
		if m, has := bound("exclusiveMaximum"); has && f >= m {
			out = append(out, fmt.Sprintf("%s must be < %s", path, formatJSNumber(m)))
		}
		if m, has := bound("exclusiveMinimum"); has && f <= m {
			out = append(out, fmt.Sprintf("%s must be > %s", path, formatJSNumber(m)))
		}
	}
	if str, ok := value.(string); ok {
		n := utf8.RuneCountInString(str)
		if m, has := bound("maxLength"); has && float64(n) > m {
			out = append(out, fmt.Sprintf("%s must NOT have more than %s characters", path, formatJSNumber(m)))
		}
		if m, has := bound("minLength"); has && float64(n) < m {
			out = append(out, fmt.Sprintf("%s must NOT have fewer than %s characters", path, formatJSNumber(m)))
		}
		if pat, ok := sch["pattern"].(string); ok {
			if re, err := regexp.Compile(pat); err == nil && !re.MatchString(str) {
				out = append(out, fmt.Sprintf("%s must match pattern %q", path, pat))
			}
		}
	}
	if arr, ok := value.([]interface{}); ok {
		if m, has := bound("maxItems"); has && float64(len(arr)) > m {
			out = append(out, fmt.Sprintf("%s must NOT have more than %s items", path, formatJSNumber(m)))
		}
		if m, has := bound("minItems"); has && float64(len(arr)) < m {
			out = append(out, fmt.Sprintf("%s must NOT have fewer than %s items", path, formatJSNumber(m)))
		}
	}
	if obj, ok := value.(map[string]interface{}); ok {
		if m, has := bound("maxProperties"); has && float64(len(obj)) > m {
			out = append(out, fmt.Sprintf("%s must NOT have more than %s properties", path, formatJSNumber(m)))
		}
		if m, has := bound("minProperties"); has && float64(len(obj)) < m {
			out = append(out, fmt.Sprintf("%s must NOT have fewer than %s properties", path, formatJSNumber(m)))
		}
	}
	return out
}

// optionsOrArgs returns the tool's provider options, falling back to Args for
// definitions serialized before ProviderOptions had its own field.
func optionsOrArgs(options, args map[string]interface{}) interface{} {
	if options != nil {
		return options
	}
	if args != nil {
		return args
	}
	return nil
}
