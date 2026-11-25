package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

type r67NilDecisionProvider struct{}

func (r67NilDecisionProvider) DecisionModel(string) (provider.DecisionModel, error) {
	return nil, nil
}

// TS resolveDecisionModel throws NoSuchModelError({ modelId, modelType:
// 'decisionModel' }) when the provider's factory returns no model.
func TestExperimentalDecide_R67_ResolveNoSuchModelError(t *testing.T) {
	q := map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "x"}}

	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: "missing", DecisionProvider: r67NilDecisionProvider{}, State: "s", Questions: q,
	})
	var nsm *providererrors.NoSuchModelError
	if !errors.As(err, &nsm) {
		t.Fatalf("nil model: err = %T %v, want *NoSuchModelError", err, err)
	}
	if nsm.ModelID != "missing" || nsm.ModelType != "decisionModel" || err.Error() != "No such decisionModel: missing" {
		t.Errorf("nil model: got %+v / %q", nsm, err.Error())
	}

	// The deprecated Provider reports a missing model the same way.
	_, err = ExperimentalDecide(context.Background(), DecideOptions{
		Model: "gone", Provider: r67NilEvaluationProvider{}, State: "s", Questions: q, //nolint:staticcheck // deprecated compatibility path
	})
	if !errors.As(err, &nsm) || err.Error() != "No such decisionModel: gone" {
		t.Fatalf("legacy nil model: err = %T %v, want *NoSuchModelError", err, err)
	}
}

type r67NilEvaluationProvider struct{}

func (r67NilEvaluationProvider) EvaluationModel(string) (provider.EvaluationModel, error) { //nolint:staticcheck // deprecated compatibility path
	return nil, nil
}

// The adapted legacy provider (AdaptEvaluationModelProvider) reports a missing
// model as NoSuchModelError too.
func TestAdaptEvaluationModelProvider_R67_NilModelIsNoSuchModelError(t *testing.T) {
	p := provider.AdaptEvaluationModelProvider(r67NilEvaluationProvider{}) //nolint:staticcheck // deprecated compatibility path
	_, err := p.DecisionModel("gone")
	var nsm *providererrors.NoSuchModelError
	if !errors.As(err, &nsm) || err.Error() != "No such decisionModel: gone" {
		t.Fatalf("err = %T %v, want *NoSuchModelError", err, err)
	}
}
