package gateway

// Deprecated pre-rename "evaluation" names, kept as aliases (TS #21935 keeps
// the same deprecated aliases).

const (
	// EvaluationFallbackMaxConditionDepth is the old name of DecisionFallbackMaxConditionDepth.
	//
	// Deprecated: Use DecisionFallbackMaxConditionDepth instead.
	EvaluationFallbackMaxConditionDepth = DecisionFallbackMaxConditionDepth
	// EvaluationFallbackMaxConditionsPerList is the old name of DecisionFallbackMaxConditionsPerList.
	//
	// Deprecated: Use DecisionFallbackMaxConditionsPerList instead.
	EvaluationFallbackMaxConditionsPerList = DecisionFallbackMaxConditionsPerList
	// EvaluationFallbackMaxQuestionLength is the old name of DecisionFallbackMaxQuestionLength.
	//
	// Deprecated: Use DecisionFallbackMaxQuestionLength instead.
	EvaluationFallbackMaxQuestionLength = DecisionFallbackMaxQuestionLength

	// GatewayEvaluationModelLiquidD1 is the old name of GatewayDecisionModelLiquidD1.
	//
	// Deprecated: Use GatewayDecisionModelLiquidD1 instead.
	GatewayEvaluationModelLiquidD1 = GatewayDecisionModelLiquidD1
	// GatewayEvaluationModelTypesafeAiJev is the old name of GatewayDecisionModelTypesafeAiJev.
	//
	// Deprecated: Use GatewayDecisionModelTypesafeAiJev instead.
	GatewayEvaluationModelTypesafeAiJev = GatewayDecisionModelTypesafeAiJev
)

// EvaluationFallbackCondition is an alias of DecisionFallbackCondition.
//
// Deprecated: Use DecisionFallbackCondition instead.
type EvaluationFallbackCondition = DecisionFallbackCondition

// EvaluationFallbackAtLeast is an alias of DecisionFallbackAtLeast.
//
// Deprecated: Use DecisionFallbackAtLeast instead.
type EvaluationFallbackAtLeast = DecisionFallbackAtLeast

// GatewayEvaluationModelID is an alias of GatewayDecisionModelID.
//
// Deprecated: Use GatewayDecisionModelID instead.
type GatewayEvaluationModelID = GatewayDecisionModelID

// GatewayEvaluationModelIDs is the old name of GatewayDecisionModelIDs.
//
// Deprecated: Use GatewayDecisionModelIDs instead.
var GatewayEvaluationModelIDs = GatewayDecisionModelIDs

// EvaluationModel is an alias of DecisionModel.
//
// Deprecated: Use DecisionModel instead.
type EvaluationModel = DecisionModel

// NewEvaluationModel is the old name of NewDecisionModel.
//
// Deprecated: Use NewDecisionModel instead.
func NewEvaluationModel(p *Provider, modelID string) *EvaluationModel {
	return NewDecisionModel(p, modelID)
}
