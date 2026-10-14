package gateway

import "testing"

// The pre-rename evaluation names must keep compiling and mean the same thing.
func TestDeprecatedEvaluationAliases(t *testing.T) {
	var id GatewayEvaluationModelID = GatewayEvaluationModelLiquidD1
	if id != GatewayDecisionModelLiquidD1 || len(GatewayEvaluationModelIDs) != len(GatewayDecisionModelIDs) {
		t.Fatalf("alias mismatch: %v", id)
	}
	if EvaluationFallbackMaxConditionDepth != DecisionFallbackMaxConditionDepth ||
		EvaluationFallbackMaxConditionsPerList != DecisionFallbackMaxConditionsPerList ||
		EvaluationFallbackMaxQuestionLength != DecisionFallbackMaxQuestionLength {
		t.Fatal("fallback limit aliases differ")
	}
	threshold := 0.5
	var when EvaluationFallbackCondition = DecisionFallbackCondition{ConfidenceBelow: &threshold}
	fallback := GatewayConditionalModelFallback("openai/gpt-5.6-sol", when)
	if fallback.When == nil {
		t.Fatal("conditional fallback lost its condition")
	}
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	var model *EvaluationModel = NewEvaluationModel(p, "m")
	legacy, err := p.EvaluationModel("m")
	if err != nil || legacy.ModelID() != model.ModelID() {
		t.Fatalf("legacy factory = %v %v", legacy, err)
	}
}
