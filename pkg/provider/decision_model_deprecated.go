package provider

import (
	"context"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file keeps the pre-rename "evaluation" names as deprecated aliases,
// mirroring the deprecated aliases the TypeScript SDK keeps after renaming
// experimental_evaluate to experimental_decide (vercel/ai #21935).

// EvaluationQuestion is an alias of DecisionQuestion.
//
// Deprecated: Use DecisionQuestion instead.
type EvaluationQuestion = DecisionQuestion

// EvaluationAnswer is an alias of DecisionAnswer.
//
// Deprecated: Use DecisionAnswer instead.
type EvaluationAnswer = DecisionAnswer

// EvaluationRounding is an alias of DecisionRounding.
//
// Deprecated: Use DecisionRounding instead.
type EvaluationRounding = DecisionRounding

// EvaluationUsage is an alias of DecisionUsage.
//
// Deprecated: Use DecisionUsage instead.
type EvaluationUsage = DecisionUsage

// EvaluationResponseInfo is an alias of DecisionResponseInfo.
//
// Deprecated: Use DecisionResponseInfo instead.
type EvaluationResponseInfo = DecisionResponseInfo

// EvaluationCallOptions is an alias of DecisionCallOptions.
//
// Deprecated: Use DecisionCallOptions instead.
type EvaluationCallOptions = DecisionCallOptions

// EvaluationResult is an alias of DecisionResult.
//
// Deprecated: Use DecisionResult instead.
type EvaluationResult = DecisionResult

// EvaluationModel is the pre-rename model contract whose call method is
// DoEvaluate. Existing implementations keep working: the SDK adapts them to
// DecisionModel wherever a decision model is accepted.
//
// Deprecated: Implement DecisionModel with DoDecide instead.
type EvaluationModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string
	SupportedQuestionTypes() []string

	// DoEvaluate has the same contract as DecisionModel.DoDecide.
	DoEvaluate(ctx context.Context, opts DecisionCallOptions) (*DecisionResult, error)
}

// EvaluationModelProvider is the pre-rename structural provider extension.
// Wrap one with AdaptEvaluationModelProvider to use it where a
// DecisionModelProvider is expected.
//
// Deprecated: Implement DecisionModelProvider instead.
type EvaluationModelProvider interface {
	EvaluationModel(modelID string) (EvaluationModel, error)
}

type evaluationToDecisionModel struct {
	EvaluationModel
}

func (m evaluationToDecisionModel) DoDecide(ctx context.Context, opts DecisionCallOptions) (*DecisionResult, error) {
	opts.State = legacyEvaluationState(opts.State)
	return m.DoEvaluate(ctx, opts)
}

// legacyEvaluationState restores the pre-multimodal state shape for models
// that only implement DoEvaluate. Core now always passes ordered
// []DecisionStatePart; a state made of exactly one text part is handed back
// as the string and one json part as its JSON value, which is what these
// models received before. Multi-part or file states are new and are passed
// through as parts. (TS passes parts through unchanged; Go converts so
// existing type-asserting models keep working.)
func legacyEvaluationState(state interface{}) interface{} {
	parts, ok := state.([]DecisionStatePart)
	if !ok || len(parts) != 1 {
		return state
	}
	switch parts[0].Type {
	case "text":
		return parts[0].Text
	case "json":
		return parts[0].Value
	}
	return state
}

// AdaptEvaluationModel adapts a legacy EvaluationModel to a DecisionModel by
// routing DoDecide to DoEvaluate. A model that already implements
// DecisionModel is returned unchanged. Mirrors the legacy-model adapter in
// TypeScript's resolveDecisionModel.
//
// Deprecated: Implement DecisionModel instead.
func AdaptEvaluationModel(model EvaluationModel) DecisionModel {
	if model == nil {
		return nil
	}
	if decision, ok := model.(DecisionModel); ok {
		return decision
	}
	return evaluationToDecisionModel{EvaluationModel: model}
}

type decisionToEvaluationModel struct {
	DecisionModel
}

func (m decisionToEvaluationModel) DoEvaluate(ctx context.Context, opts DecisionCallOptions) (*DecisionResult, error) {
	return m.DoDecide(ctx, opts)
}

// AsEvaluationModel exposes a DecisionModel through the deprecated
// EvaluationModel interface (DoEvaluate delegates to DoDecide). Mirrors
// TypeScript's asEvaluationModel. A model that already implements
// EvaluationModel is returned unchanged.
//
// Deprecated: Use DecisionModel instead.
func AsEvaluationModel(model DecisionModel) EvaluationModel {
	if model == nil {
		return nil
	}
	if legacy, ok := model.(EvaluationModel); ok {
		return legacy
	}
	return decisionToEvaluationModel{DecisionModel: model}
}

type evaluationToDecisionProvider struct {
	EvaluationModelProvider
}

func (p evaluationToDecisionProvider) DecisionModel(modelID string) (DecisionModel, error) {
	model, err := p.EvaluationModel(modelID)
	if err != nil {
		return nil, err
	}
	if model == nil {
		// TS resolveDecisionModel: NoSuchModelError({ modelId, modelType: 'decisionModel' }).
		return nil, providererrors.NewNoSuchModelError(modelID, "decisionModel")
	}
	return AdaptEvaluationModel(model), nil
}

// AdaptEvaluationModelProvider adapts a legacy EvaluationModelProvider to a
// DecisionModelProvider. A provider that already implements
// DecisionModelProvider is returned unchanged.
//
// Deprecated: Implement DecisionModelProvider instead.
func AdaptEvaluationModelProvider(p EvaluationModelProvider) DecisionModelProvider {
	if p == nil {
		return nil
	}
	if decision, ok := p.(DecisionModelProvider); ok {
		return decision
	}
	return evaluationToDecisionProvider{EvaluationModelProvider: p}
}

// DecisionModelFromProvider resolves a decision model through p, preferring
// DecisionModel and falling back to the deprecated EvaluationModel factory.
// ok is false when p implements neither. Mirrors TypeScript's
// `provider.decisionModel ?? provider.evaluationModel`.
func DecisionModelFromProvider(p interface{}, modelID string) (model DecisionModel, ok bool, err error) {
	switch v := p.(type) {
	case DecisionModelProvider:
		model, err = v.DecisionModel(modelID)
		return model, true, err
	case EvaluationModelProvider:
		legacy, lerr := v.EvaluationModel(modelID)
		if lerr != nil {
			return nil, true, lerr
		}
		if legacy == nil {
			return nil, true, nil
		}
		return AdaptEvaluationModel(legacy), true, nil
	default:
		return nil, false, nil
	}
}

// RegisterEvaluationModelDeserializer registers a factory for the deprecated
// EvaluationModel contract. The factory's result is adapted to DecisionModel.
//
// Deprecated: Use RegisterDecisionModelDeserializer instead.
func RegisterEvaluationModelDeserializer(providerName string, fn func(SerializedModel) (EvaluationModel, error)) {
	RegisterDecisionModelDeserializer(providerName, func(serialized SerializedModel) (DecisionModel, error) {
		model, err := fn(serialized)
		if err != nil {
			return nil, err
		}
		return AdaptEvaluationModel(model), nil
	})
}

// DeserializeEvaluationModel reconstructs a serialized decision model through
// the deprecated EvaluationModel interface.
//
// Deprecated: Use DeserializeDecisionModel instead.
func DeserializeEvaluationModel(serialized SerializedModel) (EvaluationModel, error) {
	model, err := DeserializeDecisionModel(serialized)
	if err != nil {
		return nil, err
	}
	return AsEvaluationModel(model), nil
}

// RegisteredEvaluationModelDeserializerKeys returns every registered decision
// model deserializer key.
//
// Deprecated: Use RegisteredDecisionModelDeserializerKeys instead.
func RegisteredEvaluationModelDeserializerKeys() []string {
	return RegisteredDecisionModelDeserializerKeys()
}

// SerializeEvaluationModel serializes a legacy EvaluationModel.
//
// Deprecated: Use SerializeDecisionModel instead.
func SerializeEvaluationModel(model EvaluationModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}
