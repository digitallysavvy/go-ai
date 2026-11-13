// Package zodjson renders a wire JSON body the way TS JSON.stringify renders
// the value a zod object schema parsed from it: the schema's keys in schema
// order, none of the unknown keys the wire carried, explicit nulls kept and
// absent optional keys omitted. Providers use it for error messages that embed
// the parsed response.
package zodjson

import "github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"

// Node describes the shape of a zod object (Keys, with Child shapes for
// nested objects) or of an array of objects (Item).
type Node struct {
	Keys  []string
	Child map[string]*Node
	Item  *Node
}

// Obj builds an object node.
func Obj(keys []string, child map[string]*Node) *Node { return &Node{Keys: keys, Child: child} }

// Array builds an array-of-object node.
func Array(item *Node) *Node { return &Node{Item: item} }

// Stringify renders raw as JSON.stringify(schema.parse(raw)) would. A body
// that is not valid JSON is returned unchanged.
func Stringify(raw []byte, spec *Node) string {
	tree, ok := jsonorder.Parse(raw)
	if !ok {
		return string(raw)
	}
	return jsonorder.Stringify(pick(tree, spec))
}

func pick(v interface{}, spec *Node) interface{} {
	if spec == nil {
		return v
	}
	switch x := v.(type) {
	case *jsonorder.Object:
		if spec.Keys == nil {
			return v
		}
		out := jsonorder.New()
		for _, k := range spec.Keys {
			if val, ok := x.Get(k); ok {
				out.Set(k, pick(val, spec.Child[k]))
			}
		}
		return out
	case []interface{}:
		if spec.Item == nil {
			return v
		}
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = pick(e, spec.Item)
		}
		return out
	}
	return v
}
