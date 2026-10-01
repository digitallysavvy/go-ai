package providerutils

import (
	"encoding/json"
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
