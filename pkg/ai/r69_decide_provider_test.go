package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r69V052EvaluationProvider has the exact v0.5.2 EvaluationModelProvider
// shape (EvaluationModel(id) (provider.EvaluationModel, error)).
type r69V052EvaluationProvider struct{ model provider.EvaluationModel } //nolint:staticcheck // v0.5.2 shape

func (p r69V052EvaluationProvider) EvaluationModel(string) (provider.EvaluationModel, error) { //nolint:staticcheck // v0.5.2 shape
	return p.model, nil
}

// r69DualProvider implements both factories (like the built-in providers).
type r69DualProvider struct {
	mu       sync.Mutex
	decision int
	legacy   int
	model    *legacyEvaluationModel
}

func (p *r69DualProvider) DecisionModel(string) (provider.DecisionModel, error) {
	p.mu.Lock()
	p.decision++
	p.mu.Unlock()
	return provider.AdaptEvaluationModel(p.model), nil //nolint:staticcheck // test adapter
}

func (p *r69DualProvider) EvaluationModel(string) (provider.EvaluationModel, error) { //nolint:staticcheck // v0.5.2 shape
	p.mu.Lock()
	p.legacy++
	p.mu.Unlock()
	return p.model, nil
}

// v0.5.2 code that reads EvaluateOptions.Provider as a typed
// provider.EvaluationModelProvider must keep compiling (owner ruling
// 2026-10-30: Provider keeps its exact v0.5.2 type).
func TestDecideOptions_R69_V052ProviderShapeCompiles(t *testing.T) {
	model := &legacyEvaluationModel{}
	opts := EvaluateOptions{ //nolint:staticcheck // v0.5.2 shape
		Model:     "legacy-model",
		Provider:  r69V052EvaluationProvider{model: model}, //nolint:staticcheck // v0.5.2 shape
		State:     "message",
		Questions: legacyQuestions(),
	}
	var p provider.EvaluationModelProvider = opts.Provider                               //nolint:staticcheck // v0.5.2 shape
	if m, err := opts.Provider.EvaluationModel("legacy-model"); err != nil || m == nil { //nolint:staticcheck // v0.5.2 shape
		t.Fatalf("EvaluationModel() = %v, %v", m, err)
	}
	_ = p
	out, err := ExperimentalEvaluate(context.Background(), opts) //nolint:staticcheck // v0.5.2 shape
	if err != nil {
		t.Fatal(err)
	}
	if out.Answers["route"].Choice != "billing" || model.calls != 1 {
		t.Fatalf("answers=%+v calls=%d", out.Answers, model.calls)
	}
}

// DecisionProvider wins over Provider when both are set.
func TestDecideOptions_R69_DecisionProviderWins(t *testing.T) {
	winner := &r69DualProvider{model: &legacyEvaluationModel{}}
	loser := &r69DualProvider{model: &legacyEvaluationModel{}}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:            "m",
		DecisionProvider: winner,
		Provider:         loser, //nolint:staticcheck // deprecated compatibility path
		State:            "message",
		Questions:        legacyQuestions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner.decision != 1 || loser.decision != 0 || loser.legacy != 0 {
		t.Fatalf("winner=%d loser decision=%d legacy=%d", winner.decision, loser.decision, loser.legacy)
	}
}

// A deprecated Provider that also implements DecisionModel resolves through
// DecisionModel (TS `provider.decisionModel ?? provider.evaluationModel`).
func TestDecideOptions_R69_ProviderPrefersDecisionFactory(t *testing.T) {
	dual := &r69DualProvider{model: &legacyEvaluationModel{}}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:     "m",
		Provider:  dual, //nolint:staticcheck // deprecated compatibility path
		State:     "message",
		Questions: legacyQuestions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dual.decision != 1 || dual.legacy != 0 {
		t.Fatalf("decision=%d legacy=%d", dual.decision, dual.legacy)
	}
}
