package providerutils

import (
	"errors"
	"math"
	"math/big"
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
			// big.Int has no 64-bit limit; JS rounds the exact value to the
			// nearest float64.
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || n.Sign() < 0 {
				return 0, false
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f, true
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

// JSNumberString is JavaScript's String(number) (Number::toString, radix 10):
// the shortest round-trip digits, plain notation for exponents in (-7, 21]
// and "1e+21" / "1e-7" style otherwise; NaN and the infinities are "NaN",
// "Infinity" and "-Infinity".
func JSNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// "d.ddde±xx" with the shortest digits that round-trip.
	exp := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expText, _ := strings.Cut(exp, "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(expText)
	k := len(digits)
	n := e + 1 // value = 0.digits * 10^n
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	abs := n - 1
	if abs < 0 {
		abs = -abs
	}
	if k == 1 {
		return sign + digits + "e" + expSign + strconv.Itoa(abs)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + expSign + strconv.Itoa(abs)
}
