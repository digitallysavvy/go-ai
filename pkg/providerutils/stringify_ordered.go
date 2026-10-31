package providerutils

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// StringifyOrdered is JSON.stringify for a request body that TS builds as an
// object literal: a Go map has no key order, so orders lists the JS insertion
// order per object path ("" is the root, "voice" a root key, "a.b" a nested
// one; an array element shares its array's path). Listed keys come first in that order; keys the list does not name
// (user-supplied options, whose order a Go map cannot carry) follow sorted.
// HTML characters stay literal like JSON.stringify.
func StringifyOrdered(v interface{}, orders map[string][]string) string {
	return jsonorder.Stringify(orderTree(v, "", orders))
}

func orderTree(v interface{}, path string, orders map[string][]string) interface{} {
	switch x := v.(type) {
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = orderTree(e, path, orders)
		}
		return out
	case []map[string]interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = orderTree(e, path, orders)
		}
		return out
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	obj := jsonorder.New()
	seen := map[string]bool{}
	for _, k := range orders[path] {
		if val, ok := m[k]; ok {
			seen[k] = true
			obj.Set(k, orderTree(val, joinPath(path, k), orders))
		}
	}
	rest := map[string]interface{}{}
	for k, val := range m {
		if !seen[k] {
			rest[k] = val
		}
	}
	for _, k := range jsonorder.FromMap(rest).Keys() {
		obj.Set(k, orderTree(rest[k], joinPath(path, k), orders))
	}
	return obj
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
