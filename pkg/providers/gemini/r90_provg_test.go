package gemini

import (
	"encoding/json"
	"math"
	"testing"
)

func r90provgStr(s string) *string   { return &s }
func r90provgNum(n float64) *float64 { return &n }
func r90provgBool(b bool) *bool      { return &b }

// r90provgAccumulate feeds one arg per chunk through processPartialArgs (the
// stream path, which fails the stream on a path conflict).
func r90provgAccumulate(t *testing.T, args []PartialArg) (text, finalJSON string) {
	t.Helper()
	acc := NewGoogleJSONAccumulator()
	for _, a := range args {
		res, err := acc.processPartialArgs([]PartialArg{a})
		if err != nil {
			t.Fatalf("processPartialArgs(%s): %v", a.JSONPath, err)
		}
		text += res.TextDelta
	}
	final, closing := acc.Finalize()
	return text + closing, final
}

// TestR90provgDottedArrayIndexes ports TS 304dec5081
// (google-json-accumulator.test.ts "array index bounds"): dotted numeric
// segments on an existing array are array indexes, checked for contiguity.
func TestR90provgDottedArrayIndexes(t *testing.T) {
	cases := []struct {
		name string
		args []PartialArg
		want string
	}{
		{"skip dotted indexes that leave a gap in an existing array", []PartialArg{
			{JSONPath: "$.items[0]", StringValue: r90provgStr("a")},
			{JSONPath: "$.items.3", StringValue: r90provgStr("ignored")},
			{JSONPath: "$.items[1]", StringValue: r90provgStr("b")},
		}, `{"items":["a","b"]}`},
		{"skip dotted indexes that leave a gap in nested arrays", []PartialArg{
			{JSONPath: "$.rows[0][0]", NumberValue: r90provgNum(1)},
			{JSONPath: "$.rows.0.3", NumberValue: r90provgNum(2)},
			{JSONPath: "$.rows.3.name", StringValue: r90provgStr("ignored")},
			{JSONPath: "$.rows[0][1]", NumberValue: r90provgNum(3)},
		}, `{"rows":[[1,3]]}`},
		{"skip dotted indexes that leave a gap in an empty array", []PartialArg{
			{JSONPath: "$.items[]"},
			{JSONPath: "$.items.2"},
			{JSONPath: "$.items[0]", NumberValue: r90provgNum(1)},
		}, `{"items":[1]}`},
		{"stream contiguous dotted indexes in existing arrays", []PartialArg{
			{JSONPath: "$.rows[0][0]", StringValue: r90provgStr("a"), WillContinue: r90provgBool(true)},
			{JSONPath: "$.rows.0.0", StringValue: r90provgStr("b")},
			{JSONPath: "$.rows.0.1", StringValue: r90provgStr("c")},
			{JSONPath: "$.rows.1[0]", StringValue: r90provgStr("d")},
		}, `{"rows":[["ab","c"],["d"]]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, final := r90provgAccumulate(t, tc.args)
			if final != tc.want {
				t.Fatalf("finalJSON = %s, want %s", final, tc.want)
			}
			if text != final {
				t.Fatalf("text = %s, want %s", text, final)
			}
		})
	}

	for _, key := range []string{"0", "3", "01", "-0", "1e0"} {
		t.Run("preserve dotted object key "+key, func(t *testing.T) {
			text, final := r90provgAccumulate(t, []PartialArg{{JSONPath: "$.items." + key + ".name", StringValue: r90provgStr("a")}})
			b, _ := json.Marshal(map[string]any{"items": map[string]any{key: map[string]any{"name": "a"}}})
			if final != string(b) || text != final {
				t.Fatalf("finalJSON = %s, text = %s, want %s", final, text, b)
			}
		})
	}
}

func TestR90provgCanonicalArrayIndex(t *testing.T) {
	for key, want := range map[string]bool{"0": true, "7": true, "4294967294": true, "4294967295": false, "01": false, "-0": false, "1e0": false, "": false, " 1": false} {
		if _, ok := canonicalArrayIndex(key); ok != want {
			t.Errorf("canonicalArrayIndex(%q) = %v, want %v", key, ok, want)
		}
	}
}

// TestR90provgOptionNumbers: googleLanguageModelOptions number fields are
// z.number(): every Go integer kind is a number, and zod 4 rejects NaN and
// ±Infinity (google-language-model-options.ts thinkingBudget).
func TestR90provgOptionNumbers(t *testing.T) {
	for _, v := range []interface{}{int8(1), int16(2), uint8(3), uint16(4), 5, 6.5, float32(7)} {
		if err := checkLanguageModelOptions(map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingBudget": v}}); err != nil {
			t.Errorf("thinkingBudget %T(%v) rejected: %v", v, v, err)
		}
	}
	for _, v := range []interface{}{math.Inf(1), math.Inf(-1), math.NaN(), "1"} {
		if err := checkLanguageModelOptions(map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingBudget": v}}); err == nil {
			t.Errorf("thinkingBudget %v accepted", v)
		}
	}
}

// TestR90provgSanitizeNullDefs: TS sanitizeResponseJsonSchema keeps $defs in
// its rest object, so a null $defs is sent as null (only a non-null one is
// sanitized).
func TestR90provgSanitizeNullDefs(t *testing.T) {
	out := sanitizeResponseJSONSchema(json.RawMessage(`{"type":"object","$defs":null,"properties":{"a":{"const":1}}}`))
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"object","$defs":null,"properties":{"a":{"enum":[1]}}}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}
