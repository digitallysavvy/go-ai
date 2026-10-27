package telemetry

import (
	"context"
	"math"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// TS sanitizeAttributes (then OTel's attribute check): objects and null are
// dropped, a mixed array keeps its primitives only when they share one type,
// and numeric kinds Go has but JS does not are still numbers.
func TestEnrichSpanValuesSanitizedLikeTS(t *testing.T) {
	enrich := func(context.Context, EnrichSpanOptions) map[string]interface{} {
		return map[string]interface{}{
			"obj":        map[string]interface{}{"a": 1},
			"null":       nil,
			"strs":       []interface{}{"a", nil, "b"},
			"mixed":      []interface{}{"a", 1.0},
			"nums":       []interface{}{1.0, 2},
			"bad_nums":   []interface{}{1.0, math.Inf(1)},
			"objs":       []interface{}{map[string]interface{}{}},
			"i32":        int32(7),
			"f32":        float32(1.5),
			"struct":     struct{ A int }{1},
			"plain_bool": true,
		}
	}
	got := map[string]attribute.Value{}
	for _, kv := range customSpanAttributes(context.Background(), enrich, nil, EnrichSpanOptions{}) {
		got[string(kv.Key)] = kv.Value
	}
	for _, dropped := range []string{"obj", "null", "mixed", "bad_nums", "objs", "struct"} {
		if v, ok := got[dropped]; ok {
			t.Errorf("%s kept as %v", dropped, v.String())
		}
	}
	if v := got["strs"]; v.Type() != attribute.STRINGSLICE || len(v.AsStringSlice()) != 2 {
		t.Errorf("strs = %v", v.String())
	}
	if v := got["nums"]; v.Type() != attribute.FLOAT64SLICE || len(v.AsFloat64Slice()) != 2 {
		t.Errorf("nums = %v", v.String())
	}
	if v := got["i32"]; v.Type() != attribute.INT64 || v.AsInt64() != 7 {
		t.Errorf("i32 = %v", v.String())
	}
	if v := got["f32"]; v.Type() != attribute.FLOAT64 || v.AsFloat64() != 1.5 {
		t.Errorf("f32 = %v", v.String())
	}
	if v := got["plain_bool"]; v.Type() != attribute.BOOL {
		t.Errorf("plain_bool = %v", v.String())
	}
}

// TS addRuntimeContextAttribute keeps arrays and primitives (then sanitizes
// them) and flattens every non-array object.
func TestRuntimeContextAttributesFlattenLikeTS(t *testing.T) {
	type nested struct {
		Plan string `json:"plan"`
	}
	got := map[string]attribute.Value{}
	for _, kv := range runtimeContextAttributes(map[string]interface{}{
		"tags":   []interface{}{"a", "b"},
		"labels": map[string]string{"env": "prod"},
		"user":   nested{Plan: "pro"},
		"n":      int32(3),
		"mixed":  []interface{}{"a", 1.0},
	}) {
		got[string(kv.Key)] = kv.Value
	}
	if v := got["ai.settings.context.tags"]; v.Type() != attribute.STRINGSLICE || len(v.AsStringSlice()) != 2 {
		t.Errorf("tags = %v", v.String())
	}
	if v := got["ai.settings.context.labels.env"]; v.AsString() != "prod" {
		t.Errorf("labels.env = %v (all %v)", v.String(), got)
	}
	if v := got["ai.settings.context.user.plan"]; v.AsString() != "pro" {
		t.Errorf("user.plan = %v", v.String())
	}
	if v := got["ai.settings.context.n"]; v.Type() != attribute.INT64 || v.AsInt64() != 3 {
		t.Errorf("n = %v", v.String())
	}
	if _, ok := got["ai.settings.context.mixed"]; ok {
		t.Error("mixed-type array kept")
	}
}
