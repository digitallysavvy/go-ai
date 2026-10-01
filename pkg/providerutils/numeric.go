package providerutils

import (
	"encoding/json"
	"math"
)

// maxIntMagnitude is 2^63, which is exactly representable as a float64 (it's
// a power of two well within float64's exponent range). The valid int64
// range is [-2^63, 2^63-1], so a finite, integral float f fits in an int64
// iff -maxIntMagnitude <= f < maxIntMagnitude. Used instead of comparing
// against float64(math.MaxInt)/float64(math.MinInt) directly, since
// math.MaxInt (2^63-1) is not itself exactly representable as a float64 and
// rounds up to 2^63 -- using it as an inclusive upper bound would let
// through a value that overflows on conversion.
const maxIntMagnitude float64 = 1 << 63

// CoerceInt extracts an int from a provider-options value of unknown numeric
// type, returning (0, false) if v is not a recognized numeric type, or if it
// is a numeric type but cannot be represented as an int without changing its
// value (a non-integral float, NaN/Inf, or a magnitude that overflows int).
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
// parsing is forgiving of both construction paths -- but it rejects a value
// it cannot represent exactly rather than silently truncating (1.5 -> 1),
// wrapping (a uint64 above math.MaxInt -> a negative int), or converting
// NaN/Inf to an implementation-specific garbage value, any of which could
// otherwise flow straight into something like time.NewTicker and panic on a
// non-positive interval.
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
		if n < math.MinInt || n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint:
		if uint64(n) > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case float32:
		return floatToInt(float64(n))
	case float64:
		// encoding/json's default decoding of a JSON number into
		// interface{}/map[string]interface{}.
		return floatToInt(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return CoerceInt(i)
		}
		if f, err := n.Float64(); err == nil {
			return floatToInt(f)
		}
		return 0, false
	default:
		return 0, false
	}
}

// floatToInt converts f to an int, rejecting any value that cannot be
// represented exactly: NaN, +/-Inf, a non-integral value (e.g. 1.5), or a
// magnitude that overflows int.
func floatToInt(f float64) (int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f != math.Trunc(f) {
		return 0, false
	}
	if f < -maxIntMagnitude || f >= maxIntMagnitude {
		return 0, false
	}
	return int(f), true
}
