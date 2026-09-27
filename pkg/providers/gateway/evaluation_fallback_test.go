package gateway

import (
	"math"
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
