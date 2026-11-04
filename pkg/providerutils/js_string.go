package providerutils

import (
	"encoding/json"
	"strings"
)

// JSString is JavaScript's String(value) for a value decoded from JSON into
// interface{} (encoding/json with float64 numbers, or json.Number): null is
// "null", a number follows Number::toString, an object is "[object Object]"
// and an array joins its elements with "," (null elements are empty). Use it
// where a TS provider passes an unvalidated (`z.any()`) JSON value on and
// later reads it as a string.
func JSString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return JSNumberString(x)
	case json.Number:
		if f, ok := JSNumber(string(x)); ok {
			return JSNumberString(f)
		}
		return string(x)
	case []interface{}:
		parts := make([]string, len(x))
		for i, el := range x {
			if el != nil {
				parts[i] = JSString(el)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}
