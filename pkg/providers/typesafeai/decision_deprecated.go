package typesafeai

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
