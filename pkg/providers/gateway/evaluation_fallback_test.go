package gateway

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

// TS: gateway-provider-options.test.ts "gatewayEvaluationProviderOptionsSchema
// accepts condition %#" — each shape is individually valid.
func TestValidateGatewayEvaluationModelsOption_AcceptsEachConditionShape(t *testing.T) {
	cases := []interface{}{
		map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
		map[string]interface{}{"question": "refunded", "probabilityBetween": []interface{}{0.4, 0.6}},
		map[string]interface{}{"any": []interface{}{
			map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
			map[string]interface{}{"question": "refunded", "probabilityBetween": []interface{}{0.4, 0.6}},
		}},
		map[string]interface{}{"all": []interface{}{
			map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
			map[string]interface{}{"question": "severity", "confidenceBelow": 0.7},
		}},
		map[string]interface{}{"atLeast": map[string]interface{}{
			"count": 2,
			"conditions": []interface{}{
				map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
				map[string]interface{}{"question": "refunded", "probabilityBetween": []interface{}{0.4, 0.6}},
				map[string]interface{}{"question": "severity", "confidenceBelow": 0.7},
			},
		}},
	}
	for i, when := range cases {
		models := []interface{}{
			map[string]interface{}{"model": "openai/gpt-5.6-sol", "when": when},
		}
		if err := validateGatewayEvaluationModelsOption(models); err != nil {
			t.Errorf("case %d: unexpected error: %v", i, err)
		}
	}
}

// TS: "keeps existing string fallback lists and service-owned options valid"
func TestValidateGatewayEvaluationProviderOptions_KeepsStringFallbackListsValid(t *testing.T) {
	value := map[string]interface{}{
		"models":             []interface{}{"openai/gpt-5.6-sol", "anthropic/claude-sonnet-5"},
		"serviceOwnedOption": true,
	}
	if err := validateGatewayEvaluationProviderOptions(value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TS: "accepts one conditional entry followed by string error fallbacks"
func TestValidateGatewayEvaluationModelsOption_AcceptsConditionalFollowedByStrings(t *testing.T) {
	models := []interface{}{
		map[string]interface{}{
			"model": "openai/gpt-5.6-sol",
			"when":  map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
		},
		"anthropic/claude-sonnet-5",
	}
	if err := validateGatewayEvaluationModelsOption(models); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TS: "rejects condition %#" — each shape must fail validation.
func TestValidateGatewayEvaluationModelsOption_RejectsEachInvalidConditionShape(t *testing.T) {
	cases := []interface{}{
		map[string]interface{}{"question": "", "confidenceBelow": 0.5},
		map[string]interface{}{"question": "intent", "confidenceBelow": math.NaN()},
		map[string]interface{}{"question": "intent", "confidenceBelow": math.Inf(1)},
		map[string]interface{}{"question": "intent", "confidenceBelow": -0.1},
		map[string]interface{}{"question": "intent", "confidenceBelow": 1.1},
		map[string]interface{}{"question": "refunded", "probabilityBetween": []interface{}{0.7, 0.3}},
		map[string]interface{}{"question": "refunded", "probabilityBetween": []interface{}{0.0, 1.0, 2.0}},
		map[string]interface{}{"any": []interface{}{}},
		map[string]interface{}{"all": []interface{}{}},
		map[string]interface{}{"atLeast": map[string]interface{}{
			"count":      0,
			"conditions": []interface{}{map[string]interface{}{"question": "intent", "confidenceBelow": 0.5}},
		}},
		map[string]interface{}{"atLeast": map[string]interface{}{
			"count":      1.5,
			"conditions": []interface{}{map[string]interface{}{"question": "intent", "confidenceBelow": 0.5}},
		}},
		map[string]interface{}{"atLeast": map[string]interface{}{
			"count":      2,
			"conditions": []interface{}{map[string]interface{}{"question": "intent", "confidenceBelow": 0.5}},
		}},
		map[string]interface{}{"question": "intent", "confidenceBelow": 0.5, "probabilityBetween": []interface{}{0.4, 0.6}},
		map[string]interface{}{"question": "intent", "confidenceBelow": 0.5, "any": []interface{}{map[string]interface{}{"question": "severity", "confidenceBelow": 0.7}}},
		map[string]interface{}{"question": "intent", "confidenceBelow": 0.5, "extra": true},
	}
	for i, when := range cases {
		models := []interface{}{
			map[string]interface{}{"model": "openai/gpt-5.6-sol", "when": when},
		}
		if err := validateGatewayEvaluationModelsOption(models); err == nil {
			t.Errorf("case %d: expected error, got nil for %#v", i, when)
		}
	}
}

// TS: "enforces the maximum condition depth"
func TestValidateGatewayEvaluationModelsOption_EnforcesMaximumConditionDepth(t *testing.T) {
	var condition interface{} = map[string]interface{}{"question": "intent", "confidenceBelow": 0.5}
	for depth := 1; depth < EvaluationFallbackMaxConditionDepth; depth++ {
		condition = map[string]interface{}{"any": []interface{}{condition}}
	}

	validModels := []interface{}{map[string]interface{}{"model": "openai/gpt-5.6-sol", "when": condition}}
	if err := validateGatewayEvaluationModelsOption(validModels); err != nil {
		t.Fatalf("valid depth should pass: %v", err)
	}

	invalidModels := []interface{}{map[string]interface{}{
		"model": "openai/gpt-5.6-sol",
		"when":  map[string]interface{}{"any": []interface{}{condition}},
	}}
	if err := validateGatewayEvaluationModelsOption(invalidModels); err == nil {
		t.Fatalf("expected depth-limit error")
	}
}

// TS: "accepts five condition levels and rejects six"
func TestValidateGatewayEvaluationModelsOption_AcceptsFiveLevelsRejectsSix(t *testing.T) {
	direct := map[string]interface{}{"question": "intent", "confidenceBelow": 0.5}
	fiveLevels := map[string]interface{}{
		"any": []interface{}{
			map[string]interface{}{
				"all": []interface{}{
					map[string]interface{}{
						"atLeast": map[string]interface{}{
							"count":      1,
							"conditions": []interface{}{map[string]interface{}{"any": []interface{}{direct}}},
						},
					},
				},
			},
		},
	}
	sixLevels := map[string]interface{}{"any": []interface{}{fiveLevels}}

	validModels := []interface{}{map[string]interface{}{"model": "openai/gpt-5.6-sol", "when": fiveLevels}}
	if err := validateGatewayEvaluationModelsOption(validModels); err != nil {
		t.Fatalf("five levels should pass: %v", err)
	}

	invalidModels := []interface{}{map[string]interface{}{"model": "openai/gpt-5.6-sol", "when": sixLevels}}
	err := validateGatewayEvaluationModelsOption(invalidModels)
	if err == nil {
		t.Fatalf("six levels should fail")
	}
	wantSubstr := "conditions can be nested at most 5 levels deep"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("err = %v, want to contain %q", err, wantSubstr)
	}
}

// TS: "rejects invalid models list %#"
func TestValidateGatewayEvaluationModelsOption_RejectsInvalidModelsList(t *testing.T) {
	conditionalFallback := map[string]interface{}{
		"model": "openai/gpt-5.6-sol",
		"when":  map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
	}
	cases := [][]interface{}{
		{map[string]interface{}{"model": "", "when": map[string]interface{}{"question": "intent", "confidenceBelow": 0.6}}},
		{conditionalFallback, conditionalFallback},
		{"anthropic/claude-sonnet-5", conditionalFallback},
	}
	for i, models := range cases {
		if err := validateGatewayEvaluationModelsOption(models); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

// TS: "bounds question length at $length"
func TestValidateGatewayEvaluationModelsOption_BoundsQuestionLength(t *testing.T) {
	tests := []struct {
		length  int
		success bool
	}{
		{EvaluationFallbackMaxQuestionLength, true},
		{EvaluationFallbackMaxQuestionLength + 1, false},
	}
	for _, tt := range tests {
		question := strings.Repeat("q", tt.length)
		models := []interface{}{map[string]interface{}{
			"model": "openai/gpt-5.6-sol",
			"when":  map[string]interface{}{"question": question, "confidenceBelow": 0.6},
		}}
		err := validateGatewayEvaluationModelsOption(models)
		if tt.success && err != nil {
			t.Errorf("length %d: unexpected error: %v", tt.length, err)
		}
		if !tt.success && err == nil {
			t.Errorf("length %d: expected error", tt.length)
		}
	}
}

// TS: "accepts any non-empty conditional model length ($length)"
func TestValidateGatewayEvaluationModelsOption_AcceptsAnyNonEmptyModelLength(t *testing.T) {
	tests := []struct {
		length  int
		success bool
	}{
		{1000, true},
		{0, false},
	}
	for _, tt := range tests {
		models := []interface{}{map[string]interface{}{
			"model": strings.Repeat("m", tt.length),
			"when":  map[string]interface{}{"question": "intent", "confidenceBelow": 0.6},
		}}
		err := validateGatewayEvaluationModelsOption(models)
		if tt.success && err != nil {
			t.Errorf("model length %d: unexpected error: %v", tt.length, err)
		}
		if !tt.success && err == nil {
			t.Errorf("model length %d: expected error", tt.length)
		}
	}
}

// TS: "bounds $name condition lists" for any/all/atLeast
func TestValidateGatewayEvaluationModelsOption_BoundsConditionLists(t *testing.T) {
	makeConditions := func(count int) []interface{} {
		out := make([]interface{}, count)
		for i := 0; i < count; i++ {
			out[i] = map[string]interface{}{"question": "q", "confidenceBelow": 0.6}
		}
		return out
	}
	wrappers := map[string]func([]interface{}) map[string]interface{}{
		"any": func(c []interface{}) map[string]interface{} { return map[string]interface{}{"any": c} },
		"all": func(c []interface{}) map[string]interface{} { return map[string]interface{}{"all": c} },
		"atLeast": func(c []interface{}) map[string]interface{} {
			return map[string]interface{}{"atLeast": map[string]interface{}{"count": 1, "conditions": c}}
		},
	}
	for name, wrap := range wrappers {
		validModels := []interface{}{map[string]interface{}{
			"model": "openai/gpt-5.6-sol",
			"when":  wrap(makeConditions(EvaluationFallbackMaxConditionsPerList)),
		}}
		if err := validateGatewayEvaluationModelsOption(validModels); err != nil {
			t.Errorf("%s: max count should pass: %v", name, err)
		}
		invalidModels := []interface{}{map[string]interface{}{
			"model": "openai/gpt-5.6-sol",
			"when":  wrap(makeConditions(EvaluationFallbackMaxConditionsPerList + 1)),
		}}
		if err := validateGatewayEvaluationModelsOption(invalidModels); err == nil {
			t.Errorf("%s: over max count should fail", name)
		}
	}
}

// TestGatewayModelFallback_MarshalJSON_PlainFallback verifies a plain
// fallback (built with GatewayModel, or a zero-value GatewayModelFallback)
// marshals as a bare model-ID string, matching TS's `string | {model,
// when}` union.
func TestGatewayModelFallback_MarshalJSON_PlainFallback(t *testing.T) {
	data, err := json.Marshal(GatewayModel("anthropic/claude-sonnet-5"))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) != `"anthropic/claude-sonnet-5"` {
		t.Fatalf("Marshal() = %s, want a bare string", data)
	}
}

// TestGatewayModelFallback_MarshalJSON_ConditionalFallback verifies a
// conditional fallback marshals as {"model", "when"}.
func TestGatewayModelFallback_MarshalJSON_ConditionalFallback(t *testing.T) {
	confidenceBelow := 0.6
	fallback := GatewayConditionalModelFallback("openai/gpt-5.6-sol", EvaluationFallbackCondition{
		Question:        "intent",
		ConfidenceBelow: &confidenceBelow,
	})
	data, err := json.Marshal(fallback)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("re-decode error = %v", err)
	}
	if decoded["model"] != "openai/gpt-5.6-sol" {
		t.Errorf("model = %#v", decoded["model"])
	}
	when, ok := decoded["when"].(map[string]interface{})
	if !ok || when["question"] != "intent" || when["confidenceBelow"] != 0.6 {
		t.Errorf("when = %#v", decoded["when"])
	}
}

// TestGatewayModelFallback_UnmarshalJSON_PlainString verifies the
// ergonomic path this ports from TS: a JSON array of plain model-ID
// strings (e.g. {"models": ["a", "b"]}, the common case before conditional
// fallbacks existed) unmarshals directly into []GatewayModelFallback
// without the caller having to wrap every entry.
func TestGatewayModelFallback_UnmarshalJSON_PlainString(t *testing.T) {
	var models []GatewayModelFallback
	if err := json.Unmarshal([]byte(`["openai/gpt-5.6-sol", "anthropic/claude-sonnet-5"]`), &models); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []GatewayModelFallback{
		{Model: "openai/gpt-5.6-sol"},
		{Model: "anthropic/claude-sonnet-5"},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

// TestGatewayModelFallback_UnmarshalJSON_Conditional verifies a
// {"model", "when"} object round-trips through Unmarshal back into an
// equivalent GatewayModelFallback (including nested any/all/atLeast
// groups), matching what MarshalJSON produces.
func TestGatewayModelFallback_UnmarshalJSON_Conditional(t *testing.T) {
	raw := `{
		"model": "openai/gpt-5.6-sol",
		"when": {
			"any": [
				{"question": "intent", "confidenceBelow": 0.6},
				{"question": "refunded", "probabilityBetween": [0.4, 0.6]}
			]
		}
	}`
	var fallback GatewayModelFallback
	if err := json.Unmarshal([]byte(raw), &fallback); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if fallback.Model != "openai/gpt-5.6-sol" {
		t.Fatalf("Model = %q", fallback.Model)
	}
	if fallback.When == nil || len(fallback.When.Any) != 2 {
		t.Fatalf("When = %#v", fallback.When)
	}
	first := fallback.When.Any[0]
	if first.Question != "intent" || first.ConfidenceBelow == nil || *first.ConfidenceBelow != 0.6 {
		t.Errorf("When.Any[0] = %#v", first)
	}
	second := fallback.When.Any[1]
	if second.Question != "refunded" || second.ProbabilityBetween == nil || *second.ProbabilityBetween != [2]float64{0.4, 0.6} {
		t.Errorf("When.Any[1] = %#v", second)
	}

	// Round-trip: re-marshaling should validate cleanly.
	remarshaled, err := json.Marshal(fallback)
	if err != nil {
		t.Fatalf("re-Marshal() error = %v", err)
	}
	var models []interface{}
	if err := json.Unmarshal([]byte("["+string(remarshaled)+"]"), &models); err != nil {
		t.Fatalf("re-decode error = %v", err)
	}
	if err := validateGatewayEvaluationModelsOption(models); err != nil {
		t.Fatalf("re-marshaled fallback failed validation: %v", err)
	}
}

// TestGatewayModelFallback_UnmarshalJSON_RejectsInvalidShapes verifies
// UnmarshalJSON rejects shapes that are neither a plain string nor a valid
// {model, when} object.
func TestGatewayModelFallback_UnmarshalJSON_RejectsInvalidShapes(t *testing.T) {
	cases := []string{
		`{}`,
		`{"when": {"question": "intent", "confidenceBelow": 0.6}}`,
		`{"model": ""}`,
		`123`,
		`true`,
	}
	for _, raw := range cases {
		var fallback GatewayModelFallback
		if err := json.Unmarshal([]byte(raw), &fallback); err == nil {
			t.Errorf("Unmarshal(%s) expected error, got none", raw)
		}
	}
}

// TestGatewayModelFallback_JSONRoundTrip_InProviderOptions verifies
// GatewayProviderOptions.Models (used from Go call sites, not just wire
// serialization) round-trips through json.Marshal/Unmarshal end to end,
// mirroring a caller that persists/reloads provider options as JSON.
func TestGatewayModelFallback_JSONRoundTrip_InProviderOptions(t *testing.T) {
	confidenceBelow := 0.6
	original := []GatewayModelFallback{
		GatewayConditionalModelFallback("openai/gpt-5.6-sol", EvaluationFallbackCondition{
			Question:        "intent",
			ConfidenceBelow: &confidenceBelow,
		}),
		GatewayModel("anthropic/claude-sonnet-5"),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded []GatewayModelFallback
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("decoded = %#v, want 2 entries", decoded)
	}
	if decoded[0].Model != "openai/gpt-5.6-sol" || decoded[0].When == nil {
		t.Errorf("decoded[0] = %#v", decoded[0])
	}
	if decoded[1] != (GatewayModelFallback{Model: "anthropic/claude-sonnet-5"}) {
		t.Errorf("decoded[1] = %#v, want plain anthropic/claude-sonnet-5", decoded[1])
	}
}
