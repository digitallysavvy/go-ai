package codemode

import (
	"testing"
	"time"
)

// TestR94integDateToJSON: a time.Time host value serializes like Date#toJSON
// (cr-r94-carry-to-stream): years outside 0..9999 in the expanded form, and
// an Invalid Date (outside +-8.64e15 ms) as null in every position, with no
// error.
func TestR94integDateToJSON(t *testing.T) {
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	invalid := time.UnixMilli(8_640_000_000_000_001)
	cases := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"year 10000", far, `"+010000-01-01T00:00:00.000Z"`},
		{"year -1", before, `"-000001-01-01T00:00:00.000Z"`},
		{"pointer", &far, `"+010000-01-01T00:00:00.000Z"`},
		{"invalid top level", invalid, `null`},
		{"invalid pointer", &invalid, `null`},
		{"invalid object value", map[string]interface{}{"d": invalid}, `{"d":null}`},
		{"invalid array element", []interface{}{invalid, 1}, `[null,1]`},
		{"range edge", time.UnixMilli(8_640_000_000_000_000), `"+275760-09-13T00:00:00.000Z"`},
	}
	for _, tc := range cases {
		got, err := toJSONPayload(tc.value, 0, "Tool output")
		if err != nil {
			t.Fatalf("%s: error %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
