package telemetry

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"go.opentelemetry.io/otel/attribute"
)

// V061 item 43: two provider usage keys that normalize to the same attribute.
// TS (otel/src/provider-usage-attributes.ts at 3fab50aa9b) writes into one
// object, so the later key in Object.entries order wins and the attribute
// appears once. Expected values below are the output of the TS function
// (bun, 2026-11-04) for the same JSON:
//
//	{"input_tokens":1,"inputTokens":2,"prompt_tokens":3} -> {"ai.usage.input_tokens":3}
//	{"details":{"b-x":1,"b_x":2,"bX":3},"2":4,"10":5}   -> {"ai.usage.2":4,"ai.usage.10":5,"ai.usage.details.b_x":3}

func r87bcarryUsageKV(attrs []attribute.KeyValue) [][2]interface{} {
	out := make([][2]interface{}, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, [2]interface{}{string(a.Key), a.Value.AsInterface()})
	}
	return out
}

func TestR87bcarryProviderUsageCollisionOneAttributeLaterKeyWins(t *testing.T) {
	t.Parallel()
	// Sorted key order equals the JSON order here, so Go matches TS exactly.
	usage := map[string]interface{}{"input_tokens": 1.0, "inputTokens": 2.0, "prompt_tokens": 3.0}
	want := [][2]interface{}{{"ai.usage.input_tokens", 3.0}}
	for i := 0; i < 50; i++ {
		got := r87bcarryUsageKV(getProviderUsageAttributes(usage, "ai.usage"))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: attrs = %v, want %v (TS: one attribute, later key wins)", i, got, want)
		}
	}
}

func TestR87bcarryProviderUsageOrderedObjectKeepsWireOrder(t *testing.T) {
	t.Parallel()
	parsed, ok := jsonorder.Parse([]byte(`{"b-x":1,"b_x":2,"bX":3}`))
	if !ok {
		t.Fatal("parse failed")
	}
	usage := map[string]interface{}{"details": parsed, "2": 4.0, "10": 5.0}
	want := [][2]interface{}{
		{"ai.usage.2", 4.0},
		{"ai.usage.10", 5.0},
		{"ai.usage.details.b_x", 3.0},
	}
	for i := 0; i < 50; i++ {
		got := r87bcarryUsageKV(getProviderUsageAttributes(usage, "ai.usage"))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: attrs = %v, want %v (TS output)", i, got, want)
		}
	}
}

func TestR87bcarryProviderUsagePlainMapFixedRule(t *testing.T) {
	t.Parallel()
	// A plain Go map has no key order: keys are visited sorted (the order
	// ai.response.usage serializes them), so "b_x" (sorted last) wins. TS
	// gives bX's 3 for the wire order above; documented known difference.
	usage := map[string]interface{}{"details": map[string]interface{}{"b-x": 1.0, "b_x": 2.0, "bX": 3.0}}
	want := [][2]interface{}{{"ai.usage.details.b_x", 2.0}}
	for i := 0; i < 50; i++ {
		got := r87bcarryUsageKV(getProviderUsageAttributes(usage, "ai.usage"))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: attrs = %v, want %v", i, got, want)
		}
	}
}

func TestR87bcarryProviderUsageJSONNumber(t *testing.T) {
	t.Parallel()
	// A json.Number is a JSON number (TS typeof 'number'); a value beyond
	// float64 range is Infinity in JS and skipped by Number.isFinite.
	usage := map[string]interface{}{"inputTokens": json.Number("7"), "huge": json.Number("1e400")}
	want := [][2]interface{}{{"ai.usage.input_tokens", 7.0}}
	if got := r87bcarryUsageKV(getProviderUsageAttributes(usage, "ai.usage")); !reflect.DeepEqual(got, want) {
		t.Fatalf("attrs = %v, want %v", got, want)
	}
}
