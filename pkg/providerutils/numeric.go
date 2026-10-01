package providerutils

import "encoding/json"

// CoerceInt extracts an int from a provider-options value of unknown numeric
// type, returning (0, false) if v is not a recognized numeric type.
//
// Provider options are commonly built by hand as Go int literals, but they
// can just as easily arrive via encoding/json.Unmarshal into a
// map[string]interface{} -- a JSON config file, or a request body forwarded
// straight into ProviderOptions. encoding/json decodes all JSON numbers as
// float64 (or json.Number, if the decoder used UseNumber()), never int. A
// bare `v.(int)` type assertion silently fails for both of those shapes and
// drops the user's value in favor of whatever default the caller falls back
// to, with no warning. CoerceInt accepts every numeric type Go code or
// encoding/json realistically produces, so a provider's provider-options
// parsing is forgiving of both construction paths.
func CoerceInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		// encoding/json's default decoding of a JSON number into
		// interface{}/map[string]interface{}.
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
		f, err := n.Float64()
		if err == nil {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}
