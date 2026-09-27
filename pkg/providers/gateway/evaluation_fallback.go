package gateway

import (
	"errors"
	"fmt"
	"math"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Bounds for EvaluationFallbackCondition trees. Mirror TS
// EVALUATION_FALLBACK_MAX_CONDITION_DEPTH /
// EVALUATION_FALLBACK_MAX_CONDITIONS_PER_LIST /
// EVALUATION_FALLBACK_MAX_QUESTION_LENGTH
// (packages/gateway/src/gateway-provider-options.ts).
const (
	EvaluationFallbackMaxConditionDepth    = 5
	EvaluationFallbackMaxConditionsPerList = 20
	EvaluationFallbackMaxQuestionLength    = 256
)

// EvaluationFallbackCondition is a condition on the primary model's
// answers, used to decide whether to rerun an evaluation against a
// conditional fallback model. Exactly one of the following combinations
// must be set: (Question + ConfidenceBelow), (Question +
// ProbabilityBetween), Any, All, or AtLeast. Groups (Any/All/AtLeast) nest
// at most EvaluationFallbackMaxConditionDepth levels deep. Mirrors TS
// EvaluationFallbackCondition.
type EvaluationFallbackCondition struct {
	// Question narrows the condition to a specific question ID. Used with
	// ConfidenceBelow or ProbabilityBetween.
	Question string

	// ConfidenceBelow triggers the fallback when the named question's
	// answer confidence is below this value (0-1). Used with Question.
	ConfidenceBelow *float64

	// ProbabilityBetween triggers the fallback when the named question's
	// answer probability falls within [min, max] (each 0-1, min <= max).
	// Used with Question.
	ProbabilityBetween *[2]float64

	// Any triggers the fallback when any nested condition matches.
	Any []EvaluationFallbackCondition

	// All triggers the fallback when every nested condition matches.
	All []EvaluationFallbackCondition

	// AtLeast triggers the fallback when at least AtLeast.Count nested
	// conditions match.
	AtLeast *EvaluationFallbackAtLeast
}

// EvaluationFallbackAtLeast is the payload for
// EvaluationFallbackCondition.AtLeast.
type EvaluationFallbackAtLeast struct {
	Count      int
	Conditions []EvaluationFallbackCondition
}

// GatewayModelFallback is a single entry in GatewayProviderOptions.Models:
// either a plain fallback model ID (When nil) or -- as the first entry
// only, and only for evaluation requests -- a conditional fallback that
// reruns the evaluation against Model when When matches the primary
// answers. Mirrors TS GatewayModelFallback.
type GatewayModelFallback struct {
	Model string
	When  *EvaluationFallbackCondition
}

// GatewayModel returns a plain (non-conditional) fallback model entry.
func GatewayModel(modelID string) GatewayModelFallback {
	return GatewayModelFallback{Model: modelID}
}

// GatewayConditionalModelFallback returns a conditional evaluation
// fallback entry. When present in GatewayProviderOptions.Models it must be
// the first entry; every other entry must be a plain GatewayModel.
func GatewayConditionalModelFallback(modelID string, when EvaluationFallbackCondition) GatewayModelFallback {
	return GatewayModelFallback{Model: modelID, When: &when}
}

func (f GatewayModelFallback) toWire() interface{} {
	if f.When == nil {
		return f.Model
	}
	return map[string]interface{}{
		"model": f.Model,
		"when":  f.When.toWire(),
	}
}

func (c EvaluationFallbackCondition) toWire() map[string]interface{} {
	switch {
	case c.ConfidenceBelow != nil:
		return map[string]interface{}{"question": c.Question, "confidenceBelow": *c.ConfidenceBelow}
	case c.ProbabilityBetween != nil:
		return map[string]interface{}{"question": c.Question, "probabilityBetween": []interface{}{c.ProbabilityBetween[0], c.ProbabilityBetween[1]}}
	case c.Any != nil:
		return map[string]interface{}{"any": conditionsToWire(c.Any)}
	case c.All != nil:
		return map[string]interface{}{"all": conditionsToWire(c.All)}
	case c.AtLeast != nil:
		return map[string]interface{}{"atLeast": map[string]interface{}{
			"count":      c.AtLeast.Count,
			"conditions": conditionsToWire(c.AtLeast.Conditions),
		}}
	default:
		return map[string]interface{}{}
	}
}

func conditionsToWire(conditions []EvaluationFallbackCondition) []interface{} {
	out := make([]interface{}, len(conditions))
	for i, c := range conditions {
		out[i] = c.toWire()
	}
	return out
}

// validateGatewayEvaluationProviderOptions validates the "gateway" entry of
// an evaluation call's providerOptions, mirroring TS
// gatewayEvaluationProviderOptionsSchema (only the "models" key is
// constrained; every other key passes through via the schema's
// `.catchall(z.unknown())`). Returns nil when there is nothing to
// validate (gatewayOptions is not an object, or has no "models" key).
func validateGatewayEvaluationProviderOptions(gatewayOptions interface{}) error {
	m, ok := gatewayOptions.(map[string]interface{})
	if !ok {
		return nil
	}
	modelsRaw, ok := m["models"]
	if !ok {
		return nil
	}
	if err := validateGatewayEvaluationModelsOption(modelsRaw); err != nil {
		return &providererrors.InvalidArgumentError{
			Field:   "providerOptions.gateway.models",
			Message: fmt.Sprintf("invalid gateway provider options: %v", err),
		}
	}
	return nil
}

// validateGatewayEvaluationModelsOption mirrors TS
// gatewayModelFallbacksSchema: an array of strings and/or at most one
// conditional fallback object, which -- if present -- must be the first
// entry.
func validateGatewayEvaluationModelsOption(value interface{}) error {
	list, ok := value.([]interface{})
	if !ok {
		return errors.New("models must be an array")
	}
	conditionalIndex := -1
	for i, entry := range list {
		switch e := entry.(type) {
		case string:
			continue
		case map[string]interface{}:
			if conditionalIndex == -1 {
				conditionalIndex = i
			} else {
				return errors.New("models supports at most one conditional evaluation fallback")
			}
			if err := validateConditionalModelFallback(e); err != nil {
				return err
			}
		default:
			return fmt.Errorf("models[%d] must be a string or a conditional fallback object", i)
		}
	}
	if conditionalIndex > 0 {
		return errors.New("a conditional evaluation fallback must be the first models entry")
	}
	return nil
}

// validateConditionalModelFallback mirrors TS conditionalModelFallbackSchema:
// a strict { model, when } object. Per TS commit e6a7996c12, model is
// validated with z.string().min(1) -- no maximum length.
func validateConditionalModelFallback(entry map[string]interface{}) error {
	for k := range entry {
		if k != "model" && k != "when" {
			return fmt.Errorf("unrecognized key %q in conditional model fallback", k)
		}
	}
	modelRaw, ok := entry["model"]
	if !ok {
		return errors.New(`conditional model fallback requires "model"`)
	}
	model, ok := modelRaw.(string)
	if !ok || len(model) < 1 {
		return errors.New(`conditional model fallback "model" must be a non-empty string`)
	}
	whenRaw, ok := entry["when"]
	if !ok {
		return errors.New(`conditional model fallback requires "when"`)
	}
	return validateEvaluationFallbackCondition(whenRaw, 1)
}

// validateEvaluationFallbackCondition mirrors TS conditionSchema(depth):
// depth starts at 1 for the top-level "when" condition. At
// EvaluationFallbackMaxConditionDepth, only direct conditions
// (question+confidenceBelow or question+probabilityBetween) are accepted;
// any/all/atLeast groups are rejected with the depth-limit message
// regardless of their contents.
func validateEvaluationFallbackCondition(value interface{}, depth int) error {
	m, ok := value.(map[string]interface{})
	if !ok {
		return errors.New("condition must be an object")
	}

	if isDirectCondition(m) {
		return validateDirectCondition(m)
	}

	if depth >= EvaluationFallbackMaxConditionDepth {
		if _, ok := m["any"]; ok {
			return fmt.Errorf("conditions can be nested at most %d levels deep", EvaluationFallbackMaxConditionDepth)
		}
		if _, ok := m["all"]; ok {
			return fmt.Errorf("conditions can be nested at most %d levels deep", EvaluationFallbackMaxConditionDepth)
		}
		if _, ok := m["atLeast"]; ok {
			return fmt.Errorf("conditions can be nested at most %d levels deep", EvaluationFallbackMaxConditionDepth)
		}
		return errors.New("invalid condition")
	}

	switch {
	case hasOnlyKey(m, "any"):
		return validateConditionGroup(m["any"], depth+1)
	case hasOnlyKey(m, "all"):
		return validateConditionGroup(m["all"], depth+1)
	case hasOnlyKey(m, "atLeast"):
		return validateAtLeast(m["atLeast"], depth+1)
	default:
		return errors.New("invalid condition")
	}
}

func isDirectCondition(m map[string]interface{}) bool {
	if len(m) != 2 {
		return false
	}
	if _, ok := m["question"]; !ok {
		return false
	}
	_, hasConfidenceBelow := m["confidenceBelow"]
	_, hasProbabilityBetween := m["probabilityBetween"]
	return hasConfidenceBelow != hasProbabilityBetween
}

func hasOnlyKey(m map[string]interface{}, key string) bool {
	if len(m) != 1 {
		return false
	}
	_, ok := m[key]
	return ok
}

func validateDirectCondition(m map[string]interface{}) error {
	question, ok := m["question"].(string)
	if !ok || len(question) < 1 || len(question) > EvaluationFallbackMaxQuestionLength {
		return fmt.Errorf("condition question must be between 1 and %d characters", EvaluationFallbackMaxQuestionLength)
	}
	if cb, ok := m["confidenceBelow"]; ok {
		v, ok := toFloat64(cb)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return errors.New("confidenceBelow must be a finite number between 0 and 1")
		}
		return nil
	}
	pb, ok := m["probabilityBetween"]
	if !ok {
		return errors.New("invalid condition")
	}
	arr, ok := toInterfaceSlice(pb)
	if !ok || len(arr) != 2 {
		return errors.New("probabilityBetween must be a two-element array")
	}
	minV, ok1 := toFloat64(arr[0])
	maxV, ok2 := toFloat64(arr[1])
	if !ok1 || !ok2 || math.IsNaN(minV) || math.IsInf(minV, 0) || math.IsNaN(maxV) || math.IsInf(maxV, 0) || minV < 0 || minV > 1 || maxV < 0 || maxV > 1 {
		return errors.New("probabilityBetween values must be finite numbers between 0 and 1")
	}
	if minV > maxV {
		return errors.New("probabilityBetween minimum must be less than or equal to maximum")
	}
	return nil
}

func validateConditionGroup(value interface{}, nextDepth int) error {
	arr, ok := value.([]interface{})
	if !ok {
		return errors.New("condition list must be an array")
	}
	if len(arr) < 1 || len(arr) > EvaluationFallbackMaxConditionsPerList {
		return fmt.Errorf("condition list must contain between 1 and %d entries", EvaluationFallbackMaxConditionsPerList)
	}
	for _, c := range arr {
		if err := validateEvaluationFallbackCondition(c, nextDepth); err != nil {
			return err
		}
	}
	return nil
}

func validateAtLeast(value interface{}, nextDepth int) error {
	m, ok := value.(map[string]interface{})
	if !ok {
		return errors.New("atLeast must be an object")
	}
	for k := range m {
		if k != "count" && k != "conditions" {
			return fmt.Errorf("unrecognized key %q in atLeast", k)
		}
	}
	countRaw, ok := m["count"]
	if !ok {
		return errors.New("atLeast requires count")
	}
	countF, ok := toFloat64(countRaw)
	if !ok || countF != math.Trunc(countF) || countF < 1 {
		return errors.New("atLeast count must be an integer >= 1")
	}
	conditionsRaw, ok := m["conditions"]
	if !ok {
		return errors.New("atLeast requires conditions")
	}
	arr, ok := conditionsRaw.([]interface{})
	if !ok || len(arr) < 1 || len(arr) > EvaluationFallbackMaxConditionsPerList {
		return fmt.Errorf("atLeast conditions must contain between 1 and %d entries", EvaluationFallbackMaxConditionsPerList)
	}
	if int(countF) > len(arr) {
		return errors.New("atLeast count cannot exceed the number of conditions")
	}
	for _, c := range arr {
		if err := validateEvaluationFallbackCondition(c, nextDepth); err != nil {
			return err
		}
	}
	return nil
}

// toInterfaceSlice accepts the common shapes a "probabilityBetween" pair
// may arrive in: []interface{} (raw provider options / JSON-decoded) or
// []float64 (Go-typed construction via our own toWire()).
func toInterfaceSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []float64:
		out := make([]interface{}, len(s))
		for i, f := range s {
			out[i] = f
		}
		return out, true
	default:
		return nil, false
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
