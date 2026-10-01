package providerutils

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCoerceInt(t *testing.T) {
	tests := []struct {
		name   string
		in     interface{}
		want   int
		wantOk bool
	}{
		{"int", int(111), 111, true},
		{"int8", int8(8), 8, true},
		{"int16", int16(16), 16, true},
		{"int32", int32(32), 32, true},
		{"int64", int64(64), 64, true},
		{"uint", uint(1), 1, true},
		{"uint8", uint8(2), 2, true},
		{"uint16", uint16(3), 3, true},
		{"uint32", uint32(4), 4, true},
		{"uint64", uint64(5), 5, true},
		{"float32", float32(222), 222, true},
		// This is the primary regression case: encoding/json decodes any
		// JSON number into interface{}/map[string]interface{} as float64.
		{"float64 from JSON", float64(333), 333, true},
		{"json.Number int", json.Number("444"), 444, true},
		{"json.Number float", json.Number("555.0"), 555, true},
		{"string", "111", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
		{"map", map[string]interface{}{}, 0, false},
		{"negative int", int(-5), -5, true},
		{"negative float64", float64(-5), -5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CoerceInt(tt.in)
			if ok != tt.wantOk || got != tt.want {
				t.Fatalf("CoerceInt(%#v) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

// TestCoerceInt_RejectsUnsafeFloats is a regression test: CoerceInt's
// float32/float64 branches used to do a bare `int(n)` conversion with no
// validation at all, which silently truncates a non-integral value (1.5 ->
// 1) and produces an implementation-specific garbage value for NaN/+-Inf or
// a magnitude that overflows int -- any of which could flow straight into
// something like time.NewTicker (pollIntervalMs/pollTimeoutMs are both
// CoerceInt callers) and panic on a non-positive interval, or silently poll
// with the wrong cadence. CoerceInt must reject these instead.
func TestCoerceInt_RejectsUnsafeFloats(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
	}{
		{"non-integral float64", float64(1.5)},
		{"non-integral float32", float32(1.5)},
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"float64 overflow (2^63)", math.Ldexp(1, 63)},
		{"float64 overflow (1e30)", float64(1e30)},
		{"float64 underflow (-2^70)", -math.Ldexp(1, 70)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := CoerceInt(tt.in); ok {
				t.Fatalf("CoerceInt(%v) = (%d, true), want (_, false)", tt.in, got)
			}
		})
	}
}

// TestCoerceInt_RejectsOverflowingUnsignedInts is a regression test: a
// uint/uint64 value above math.MaxInt silently wraps to a negative int via a
// bare `int(n)` conversion instead of being rejected.
func TestCoerceInt_RejectsOverflowingUnsignedInts(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
	}{
		{"uint64 above MaxInt", uint64(math.MaxInt64) + 1},
		{"uint64 max", uint64(math.MaxUint64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := CoerceInt(tt.in); ok {
				t.Fatalf("CoerceInt(%v) = (%d, true), want (_, false): overflow silently wrapped to a negative int", tt.in, got)
			}
		})
	}
}

// TestCoerceInt_RejectsNonIntegralAndUnsafeJSONNumber mirrors the float
// regression above for json.Number (used when a decoder is configured with
// UseNumber()): Int64() fails for "1.5" (not pure base-10 digits), and the
// previous code fell through to Float64() + a bare int(f) conversion,
// silently truncating instead of rejecting.
func TestCoerceInt_RejectsNonIntegralAndUnsafeJSONNumber(t *testing.T) {
	tests := []string{"1.5", "NaN", "Inf", "-Inf", "1e400"}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if got, ok := CoerceInt(json.Number(raw)); ok {
				t.Fatalf("CoerceInt(json.Number(%q)) = (%d, true), want (_, false)", raw, got)
			}
		})
	}
}

// TestCoerceInt_JSONRoundTrip is the closest analogue to the real bug: a
// provider-options map built by round-tripping through encoding/json (as a
// JSON config file, or a request body forwarded into ProviderOptions, would
// produce), where a plain `.(int)` assertion silently drops the value.
func TestCoerceInt_JSONRoundTrip(t *testing.T) {
	raw := []byte(`{"pollIntervalMs":111,"pollTimeoutMs":222}`)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	if _, ok := m["pollIntervalMs"].(int); ok {
		t.Fatal("test assumption violated: encoding/json produced an int, not float64")
	}

	interval, ok := CoerceInt(m["pollIntervalMs"])
	if !ok || interval != 111 {
		t.Fatalf("CoerceInt(pollIntervalMs) = (%d, %v), want (111, true)", interval, ok)
	}
	timeout, ok := CoerceInt(m["pollTimeoutMs"])
	if !ok || timeout != 222 {
		t.Fatalf("CoerceInt(pollTimeoutMs) = (%d, %v), want (222, true)", timeout, ok)
	}
}
