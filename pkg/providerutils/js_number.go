package providerutils

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// JSNumber is JavaScript's Number(string): surrounding JS whitespace is
// ignored, "" is 0, 0x/0o/0b prefixes are integers, "Infinity" is infinite and
// every other non-decimal text (including Go-only syntax such as "inf", "nan",
// "1_0" or hex floats) is NaN, reported as ok=false.
func JSNumber(s string) (float64, bool) {
	s = JSTrim(s)
	if s == "" {
		return 0, true
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			if strings.ContainsAny(s[2:], "_+-") {
				return 0, false
			}
			u, err := strconv.ParseUint(s[2:], base, 64)
			if err != nil {
				return 0, false
			}
			return float64(u), true
		}
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}
