package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// Ported from TS 8c659885c5 (#21427): otel/src/provider-usage-attributes.ts
// normalizes arbitrary provider usage objects (tokens, characters, seconds,
// ...) into numeric span attributes, aliasing common field spellings to a
// shared set of names. These tests cover the Go port directly; the
// generateSpeech/transcribe/streamTranscribe telemetry tests cover it
// end-to-end through the span attributes it produces.

func attrsToMap(attrs []attribute.KeyValue) map[string]interface{} {
	out := map[string]interface{}{}
	for _, a := range attrs {
		out[string(a.Key)] = a.Value.AsInterface()
	}
	return out
}

func TestGetProviderUsageAttributes_Aliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string
	}{
		{"inputTokens", "input_tokens"},
		{"promptTokens", "input_tokens"},
		{"promptTokenCount", "input_tokens"},
		{"totalInputTokens", "input_tokens"},
		{"outputTokens", "output_tokens"},
		{"completionTokens", "output_tokens"},
		{"completionTokenCount", "output_tokens"},
		{"candidatesTokenCount", "output_tokens"},
		{"totalTokens", "total_tokens"},
		{"totalTokenCount", "total_tokens"},
	}
	for _, tt := range tests {
		got := attrsToMap(getProviderUsageAttributes(map[string]interface{}{tt.key: 5}, "ai.usage"))
		if v, ok := got["ai.usage."+tt.want]; !ok || v != 5.0 {
			t.Errorf("normalizeUsageKey(%q): attrs = %v, want ai.usage.%s = 5", tt.key, got, tt.want)
		}
	}
}

func TestGetProviderUsageAttributes_GenericCamelToSnake(t *testing.T) {
	t.Parallel()
	got := attrsToMap(getProviderUsageAttributes(map[string]interface{}{"characters": 69}, "ai.usage"))
	if v, ok := got["ai.usage.characters"]; !ok || v != 69.0 {
		t.Errorf("attrs = %v, want ai.usage.characters = 69", got)
	}

	got = attrsToMap(getProviderUsageAttributes(map[string]interface{}{"durationSeconds": 36.744}, "ai.usage"))
	if v, ok := got["ai.usage.duration_seconds"]; !ok || v != 36.744 {
		t.Errorf("attrs = %v, want ai.usage.duration_seconds = 36.744", got)
	}
}

func TestGetProviderUsageAttributes_NestedObject(t *testing.T) {
	t.Parallel()
	usage := map[string]interface{}{
		"inputTokenDetails": map[string]interface{}{
			"cachedTokens": 3,
		},
	}
	got := attrsToMap(getProviderUsageAttributes(usage, "gen_ai.usage"))
	if v, ok := got["gen_ai.usage.input_token_details.cached_tokens"]; !ok || v != 3.0 {
		t.Errorf("attrs = %v, want gen_ai.usage.input_token_details.cached_tokens = 3", got)
	}
}

func TestGetProviderUsageAttributes_SkipsNonFiniteAndNonNumeric(t *testing.T) {
	t.Parallel()
	usage := map[string]interface{}{
		"ok":       4,
		"nan":      []interface{}{1, 2}, // not a number, not an object: dropped
		"nested":   "a string",          // not an object: dropped
		"infinite": mathInf(),
	}
	got := attrsToMap(getProviderUsageAttributes(usage, "ai.usage"))
	if len(got) != 1 {
		t.Fatalf("attrs = %v, want exactly 1 entry (ai.usage.ok)", got)
	}
	if v, ok := got["ai.usage.ok"]; !ok || v != 4.0 {
		t.Errorf("attrs = %v, want ai.usage.ok = 4", got)
	}
}

func mathInf() float64 {
	var zero float64
	return 1 / zero
}

func TestGetProviderUsageAttributes_Empty(t *testing.T) {
	t.Parallel()
	if got := getProviderUsageAttributes(nil, "ai.usage"); got != nil {
		t.Errorf("getProviderUsageAttributes(nil, ...) = %v, want nil", got)
	}
	if got := getProviderUsageAttributes(map[string]interface{}{}, "ai.usage"); got != nil {
		t.Errorf("getProviderUsageAttributes({}, ...) = %v, want nil", got)
	}
}
