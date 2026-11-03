package tool

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ResolveToolSchemas returns the tools as a provider should see them: a tool
// whose Parameters is a schema.Schema (for example
// schema.NewSimpleJSONSchema(...)) gets that schema's JSON Schema map as
// Parameters, so the provider converters, which read Parameters as a JSON
// Schema map, send the real schema. Maps and *schema.OrderedSchema values
// (which marshal in their source key order) are left as they are. The input
// slice and tools are not modified; the slice is returned unchanged when
// nothing needs resolving.
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
		} else if none {
			// A nil schema value is no Parameters (TS asSchema(undefined)).
			resolved = nil
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

// resolveParameters returns the JSON Schema map of a schema.Schema
// Parameters value (ok). none reports a schema value that is no schema: a
// typed nil pointer, a nil (or typed nil) Validator, or a nil JSON Schema,
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
	}
	return nil, false, false
}
