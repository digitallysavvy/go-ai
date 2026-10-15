package jsonorder

import (
	"encoding/json"
	"sort"
)

// Hinted is a value that marshals with the object key order of Hint, the
// ordered tree Parse returned for the raw JSON the value was derived from.
// A transform that works on Go maps (a schema sanitizer, for example) loses
// the key order of the raw input; marshaling its result through Hinted brings
// the order back for every key the hint knows, at any depth. Keys the hint
// does not know follow in sorted order. Arrays are matched by index.
//
// providerutils.OrderLike is a thin wrapper over Hinted.
type Hinted struct {
	V    interface{}
	Hint interface{}
}

// MarshalJSON implements json.Marshaler.
func (h Hinted) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(h.V)
	if err != nil {
		return nil, err
	}
	tree, ok := Parse(b)
	if !ok {
		return b, nil
	}
	return []byte(Stringify(reorder(tree, h.Hint))), nil
}

func reorder(v, hint interface{}) interface{} {
	switch x := v.(type) {
	case *Object:
		h, _ := hint.(*Object)
		out := &Object{vals: make(map[string]interface{}, len(x.keys))}
		seen := make(map[string]bool, len(x.keys))
		if h != nil {
			for _, k := range h.keys {
				if _, ok := x.vals[k]; ok && !seen[k] {
					seen[k] = true
					out.keys = append(out.keys, k)
				}
			}
		}
		rest := make([]string, 0, len(x.keys))
		for _, k := range x.keys {
			if !seen[k] {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		out.keys = append(out.keys, rest...)
		for _, k := range out.keys {
			var ch interface{}
			if h != nil {
				ch = h.vals[k]
			}
			out.vals[k] = reorder(x.vals[k], ch)
		}
		return out
	case []interface{}:
		hs, _ := hint.([]interface{})
		out := make([]interface{}, len(x))
		for i, e := range x {
			var ch interface{}
			if i < len(hs) {
				ch = hs[i]
			}
			out[i] = reorder(e, ch)
		}
		return out
	}
	return v
}

// Get walks an ordered tree from Parse by object keys and returns the value at
// the path.
func Get(v interface{}, path ...string) (interface{}, bool) {
	for _, k := range path {
		o, ok := v.(*Object)
		if !ok {
			return nil, false
		}
		v, ok = o.vals[k]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
