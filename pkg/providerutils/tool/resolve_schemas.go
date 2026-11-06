package tool

import (
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ResolveToolSchemas returns the tools as a provider should see them: a tool
// whose Parameters is a schema.Schema (for example
// schema.NewSimpleJSONSchema(...)) or a bare schema.Validator (for example
// schema.NewJSONSchema(...)) gets that schema's JSON Schema map as
// Parameters, so the provider converters, which read Parameters as a JSON
// Schema map, send the real schema. A function or dynamic tool without a
// schema (nil, typed nil or zero) gets TS asSchema(undefined).jsonSchema,
// {"type":"object","properties":{},"additionalProperties":false}; a provider
// tool keeps none. Maps and *schema.OrderedSchema values (which marshal in
// their source key order) are left as they are. The input slice and tools are
// not modified; the slice is returned unchanged when nothing needs resolving.
func ResolveToolSchemas(tools []types.Tool) []types.Tool {
	var out []types.Tool
	for i := range tools {
		var resolved interface{}
		if ordered, ok := schema.OrderedToolParameters(tools[i].Parameters, tools[i].ParameterKeyOrder); ok {
			// A schema map with a recorded key order (MCP tools).
			resolved = ordered
		} else if ordered, ok := schema.OrderedParameters(tools[i].Parameters); ok {
			// A jsonorder.Hinted schema (MCP tools) becomes an
			// *schema.OrderedSchema, which every converter sends in key order.
			resolved = ordered
		} else if m, ok, none := resolveParameters(tools[i].Parameters); ok {
			resolved = m
		} else if none || tools[i].Parameters == nil {
			// No schema: TS prepareTools sends asSchema(undefined).jsonSchema,
			// { type: 'object', properties: {}, additionalProperties: false },
			// for a function or dynamic tool.
			if !isFunctionTool(tools[i]) {
				if tools[i].Parameters == nil {
					continue
				}
				resolved = nil
			} else {
				resolved = emptyObjectSchema()
			}
		} else {
			continue
		}
		if out == nil {
			out = make([]types.Tool, len(tools))
			copy(out, tools)
		}
		out[i].Parameters = resolved
	}
	if out == nil {
		return tools
	}
	return out
}

// resolveParameters returns the JSON Schema map of a schema.Schema or a bare
// schema.Validator Parameters value (ok). none reports a schema value that is
// no schema: a typed nil pointer, a nil (or typed nil) Validator, or a nil JSON Schema,
// which callers treat as nil Parameters (TS asSchema(undefined)) instead of
// calling into the nil value.
func resolveParameters(p interface{}) (m map[string]interface{}, ok, none bool) {
	switch s := p.(type) {
	case nil, map[string]interface{}, *schema.OrderedSchema:
		return nil, false, false
	case schema.Schema:
		if m, ok := schema.ParametersMap(s); ok {
			return m, true, false
		}
		return nil, false, true
	case schema.Validator:
		// A bare validator (schema.NewJSONSchema(...)): TS
		// asSchema(inputSchema).jsonSchema is a plain JSON Schema object
		// for every schema kind.
		if isNilValidator(s) {
			return nil, false, true
		}
		if m := s.JSONSchema(); m != nil {
			return m, true, false
		}
		return nil, false, true
	}
	return nil, false, false
}

// isNilValidator reports a typed nil validator, whose methods must not be
// called.
func isNilValidator(v schema.Validator) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// isFunctionTool reports a tool TS prepareTools sends with an inputSchema
// (type 'function' or 'dynamic'; provider tools carry id and args instead).
func isFunctionTool(t types.Tool) bool {
	switch t.Type {
	case "", types.ToolTypeFunction, types.ToolTypeDynamic:
		return true
	}
	return false
}

// emptyObjectSchema is TS asSchema(undefined).jsonSchema, in its key order.
func emptyObjectSchema() *schema.OrderedSchema {
	s, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{},"additionalProperties":false}`))
	if err != nil {
		panic(err)
	}
	return s
}
