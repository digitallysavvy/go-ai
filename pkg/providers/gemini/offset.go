package gemini

import (
	"math"
	"strconv"
	"strings"
)

// JSOffsetString is JS String(value) for the wire value of a Live API
// voiceActivity.audioOffset, which TS reads with Number.parseFloat (so an
// array such as ["2s"] reads as "2s", and a boolean or object reads as text
// that parses to NaN). The second result is false for null / undefined,
// which TS treats as "no offset".
func JSOffsetString(value interface{}) (string, bool) {
	if value == nil {
		return "", false
	}
	return jsString(value), true
}

func jsString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return "Infinity"
		}
		if v == 0 {
			return "0"
		}
		if math.Abs(v) >= 1e21 || math.Abs(v) < 1e-6 {
			return strings.Replace(strconv.FormatFloat(v, 'e', -1, 64), "e+0", "e+", 1)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []interface{}:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = jsString(item)
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}
