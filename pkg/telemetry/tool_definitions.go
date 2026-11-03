package telemetry

import (
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
			def.Set("args", t.ProviderArgs)
		} else {
			def.Set("args", map[string]interface{}{})
		}
		return def
	}
	def.Set("type", "function")
	def.Set("name", t.Name)
	inputSchema, ok := toolDefinitionInputSchema(t)
	if !ok {
		// A Go tool without a schema is sent as the empty object schema.
		inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	def.Set("inputSchema", inputSchema)
	if t.Description != "" {
		def.Set("description", t.Description)
	}
	if len(t.InputExamples) > 0 {
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
