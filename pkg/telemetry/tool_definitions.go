package telemetry

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	toolutil "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// languageModelToolDefinition is a tool as TS prepareTools hands it to the
// model (LanguageModelV4FunctionTool / LanguageModelV4ProviderTool), in the
// TS key order, for the telemetry attributes that JSON.stringify those
// prepared tools (legacy ai.prompt.tools, GenAI gen_ai.tool.definitions):
// function tools {type, name, inputSchema, description?, inputExamples?,
// providerOptions?, strict?}, provider tools {type, name, id, args}.
func languageModelToolDefinition(t types.Tool) *jsonorder.Object {
	def := jsonorder.New()
	if t.Type == types.ToolTypeProviderDefined {
		def.Set("type", types.ToolTypeProviderDefined)
		def.Set("name", t.Name)
		def.Set("id", t.ProviderID)
		if t.ProviderArgs != nil {
			def.Set("args", redactProviderToolArgs(t.ProviderID, t.ProviderArgs))
		} else {
			def.Set("args", map[string]interface{}{})
		}
		return def
	}
	def.Set("type", "function")
	def.Set("name", t.Name)
	// The schema as the model receives it (TS prepareTools
	// asSchema(inputSchema).jsonSchema): GenerateText and StreamText hand
	// resolved tools already; a direct caller's nil, typed-nil or zero schema
	// becomes the TS empty object schema here too.
	t = toolutil.ResolveToolSchemas([]types.Tool{t})[0]
	inputSchema, ok := toolDefinitionInputSchema(t)
	if !ok {
		// TS asSchema(undefined).jsonSchema, as the model receives it.
		empty := jsonorder.New()
		empty.Set("type", "object")
		empty.Set("properties", jsonorder.New())
		empty.Set("additionalProperties", false)
		inputSchema = empty
	}
	def.Set("inputSchema", inputSchema)
	// TS `description != null`: Go's v0.5.2 string field cannot tell an
	// empty description from an unset one, so "" is omitted (known
	// difference).
	if t.Description != "" {
		def.Set("description", t.Description)
	}
	// TS `inputExamples != null`: a non-nil empty slice is present and
	// written as [].
	if t.InputExamples != nil {
		examples := make([]map[string]interface{}, 0, len(t.InputExamples))
		for _, ex := range t.InputExamples {
			examples = append(examples, map[string]interface{}{"input": ex.Input})
		}
		def.Set("inputExamples", examples)
	}
	if t.ProviderOptions != nil {
		def.Set("providerOptions", t.ProviderOptions)
	}
	if t.Strict != nil {
		def.Set("strict", *t.Strict)
	}
	return def
}

// harnessToolDefinition is a harness tool spec as TS turn telemetry builds
// its languageModelTools: {type: 'function', name, description?,
// inputSchema?}, with no inputSchema for a tool without one.
func harnessToolDefinition(t types.Tool) *jsonorder.Object {
	def := jsonorder.New()
	def.Set("type", "function")
	def.Set("name", t.Name)
	if t.Description != "" {
		def.Set("description", t.Description)
	}
	if inputSchema, ok := toolDefinitionInputSchema(t); ok {
		def.Set("inputSchema", inputSchema)
	}
	return def
}

// isHarnessProvider reports the provider name a harness turn reports
// (`harness:<id>`, TS turn telemetry).
func isHarnessProvider(provider string) bool {
	return strings.HasPrefix(provider, "harness:")
}

// toolDefinitionInputSchema is a tool's JSON Schema as TS
// asSchema(inputSchema).jsonSchema gives it: a schema with a key order (a
// struct schema, an *schema.OrderedSchema, an MCP schema map with its
// ParameterKeyOrder) as an *schema.OrderedSchema that marshals in that order,
// any other schema as its JSON Schema map, other values as given. ok is false
// when the tool has no schema (nil, a typed nil or a schema without a
// validator).
func toolDefinitionInputSchema(t types.Tool) (interface{}, bool) {
	if ordered, ok := schema.OrderedToolParameters(t.Parameters, t.ParameterKeyOrder); ok {
		return ordered, true
	}
	if ordered, ok := schema.OrderedParameters(t.Parameters); ok {
		return ordered, true
	}
	switch p := t.Parameters.(type) {
	case nil:
		return nil, false
	case *schema.OrderedSchema:
		return p, p != nil
	case map[string]interface{}:
		return p, p != nil
	case schema.Schema:
		m, ok := schema.ParametersMap(p)
		return m, ok
	case schema.Validator:
		if isNilValue(p) {
			return nil, false
		}
		m := p.JSONSchema()
		return m, m != nil
	}
	if m, ok := schema.ParametersMap(t.Parameters); ok {
		return m, true
	}
	if isNilValue(t.Parameters) {
		return nil, false
	}
	return t.Parameters, true
}

// isNilValue reports a nil interface or one holding a nil pointer, map,
// slice, func, chan or interface.
func isNilValue(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

const redactedToolValue = "[redacted]"

// redactProviderToolArgs removes provider credentials from a telemetry-only
// copy of a provider tool's args (TS otel redactToolDefinition, 7bb309d132):
// the openai.mcp authorization and every header value, and the value of each
// domain secret in the openai.code_interpreter container / openai.shell
// environment network policy. args itself is never modified (the provider
// request needs the real values); other tools pass through unchanged.
func redactProviderToolArgs(id string, args map[string]interface{}) map[string]interface{} {
	if id == "openai.mcp" {
		out := copyRecord(args)
		if v, ok := args["authorization"]; ok && !isNilValue(v) {
			out["authorization"] = redactedToolValue
		}
		if headers, ok := asToolRecord(args["headers"]); ok {
			redacted := make(map[string]interface{}, len(headers))
			for name := range headers {
				redacted[name] = redactedToolValue
			}
			out["headers"] = redacted
		}
		return out
	}
	var containerKey string
	switch id {
	case "openai.code_interpreter":
		containerKey = "container"
	case "openai.shell":
		containerKey = "environment"
	default:
		return args
	}
	container, ok := asToolRecord(args[containerKey])
	if !ok {
		return args
	}
	policy, ok := asToolRecord(container["networkPolicy"])
	if !ok {
		return args
	}
	secrets, ok := asToolList(policy["domainSecrets"])
	if !ok {
		return args
	}
	redacted := make([]interface{}, len(secrets))
	for i, secret := range secrets {
		if rec, ok := asToolRecord(secret); ok {
			if _, has := rec["value"]; has {
				cp := copyRecord(rec)
				cp["value"] = redactedToolValue
				redacted[i] = cp
				continue
			}
		}
		redacted[i] = secret
	}
	newPolicy := copyRecord(policy)
	newPolicy["domainSecrets"] = redacted
	newContainer := copyRecord(container)
	newContainer["networkPolicy"] = newPolicy
	out := copyRecord(args)
	out[containerKey] = newContainer
	return out
}

func copyRecord(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// asToolRecord gives v as a JSON object (TS isRecord: non-null, an object,
// not an array). Values that are not a map[string]interface{} (typed maps,
// structs, pointers) are read through their JSON form, so a credential in
// any Go shape is found.
func asToolRecord(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		return m, m != nil
	case map[string]string:
		if m == nil {
			return nil, false
		}
		out := make(map[string]interface{}, len(m))
		for k, s := range m {
			out[k] = s
		}
		return out, true
	}
	if isNilValue(v) {
		return nil, false
	}
	var out interface{}
	if b, err := json.Marshal(v); err != nil || json.Unmarshal(b, &out) != nil {
		return nil, false
	}
	m, ok := out.(map[string]interface{})
	return m, ok
}

// asToolList gives v as a JSON array (TS Array.isArray).
func asToolList(v interface{}) ([]interface{}, bool) {
	switch l := v.(type) {
	case nil:
		return nil, false
	case []interface{}:
		return l, l != nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return nil, false
	}
	out := make([]interface{}, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}
