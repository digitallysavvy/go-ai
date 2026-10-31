package telemetry

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// TS sanitizeAttributeValue drops an empty array (no primitive type) and keeps
// any all-number array; Go numeric slices of every kind are number arrays,
// named primitive types are their primitive, and a pointer is its value.
func TestEnrichSpanGoKindsSanitizedLikeTS(t *testing.T) {
	type label string
	s := "x"
	enrich := func(context.Context, EnrichSpanOptions) map[string]interface{} {
		return map[string]interface{}{
			"empty_strs": []string{},
			"empty_nums": []float64{},
			"i32s":       []int32{1, 2},
			"f32s":       []float32{1.5},
			"u16s":       []uint16{3},
			"mixed_ints": []interface{}{int8(1), "a"},
			"label":      label("prod"),
			"labels":     []label{"a", "b"},
			"ptr":        &s,
			"nil_ptr":    (*string)(nil),
			"raw":        json.RawMessage(`{"a":1}`),
			"arr":        [2]int{4, 5},
		}
	}
	got := map[string]attribute.Value{}
	for _, kv := range customSpanAttributes(context.Background(), enrich, nil, EnrichSpanOptions{}) {
		got[string(kv.Key)] = kv.Value
	}
	for _, dropped := range []string{"empty_strs", "empty_nums", "mixed_ints", "nil_ptr", "raw"} {
		if v, ok := got[dropped]; ok {
			t.Errorf("%s kept as %v", dropped, v.String())
		}
	}
	if v := got["i32s"]; v.Type() != attribute.INT64SLICE || len(v.AsInt64Slice()) != 2 {
		t.Errorf("i32s = %v", v.String())
	}
	if v := got["f32s"]; v.Type() != attribute.FLOAT64SLICE || v.AsFloat64Slice()[0] != 1.5 {
		t.Errorf("f32s = %v", v.String())
	}
	if v := got["u16s"]; v.Type() != attribute.INT64SLICE || v.AsInt64Slice()[0] != 3 {
		t.Errorf("u16s = %v", v.String())
	}
	if v := got["arr"]; v.Type() != attribute.INT64SLICE || len(v.AsInt64Slice()) != 2 {
		t.Errorf("arr = %v", v.String())
	}
	if v := got["label"]; v.Type() != attribute.STRING || v.AsString() != "prod" {
		t.Errorf("label = %v", v.String())
	}
	if v := got["labels"]; v.Type() != attribute.STRINGSLICE || len(v.AsStringSlice()) != 2 {
		t.Errorf("labels = %v", v.String())
	}
	if v := got["ptr"]; v.AsString() != "x" {
		t.Errorf("ptr = %v", v.String())
	}

	// Runtime context shares the sanitizer: an empty array is not recorded.
	for _, kv := range runtimeContextAttributes(map[string]interface{}{"tags": []string{}, "ids": []uint32{7}}) {
		if kv.Key == "ai.settings.context.tags" {
			t.Errorf("empty runtime context array kept as %v", kv.Value.String())
		}
		if kv.Key == "ai.settings.context.ids" && kv.Value.Type() != attribute.INT64SLICE {
			t.Errorf("ids = %v", kv.Value.String())
		}
	}
}
