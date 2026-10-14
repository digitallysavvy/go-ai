//nolint:staticcheck // exercises the deprecated evaluation aliases on purpose
package registry

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type mockDecisionModel struct {
	provider string
	modelID  string
}

func (m *mockDecisionModel) SpecificationVersion() string { return "v4" }
func (m *mockDecisionModel) Provider() string             { return m.provider }
func (m *mockDecisionModel) ModelID() string              { return m.modelID }
func (m *mockDecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}
func (m *mockDecisionModel) DoDecide(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return &provider.DecisionResult{}, nil
}

// decisionModelProvider wraps testutil.MockProvider with
// provider.DecisionModelProvider support for registry/custom-provider tests.
type decisionModelProvider struct {
	*testutil.MockProvider
	models map[string]provider.DecisionModel
}

func (p *decisionModelProvider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	if m, ok := p.models[modelID]; ok {
		return m, nil
	}
	return nil, noSuchModel(modelID, "decisionModel")
}

func TestCustomProvider_DecisionModel_UsesRegisteredModel(t *testing.T) {
	model := &mockDecisionModel{provider: "custom", modelID: "jev-1"}
	p := NewCustomProvider(CustomProviderOptions{
		DecisionModels: map[string]provider.DecisionModel{"jev-1": model},
	})

	ep, ok := p.(provider.DecisionModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement DecisionModelProvider")
	}
	got, err := ep.DecisionModel("jev-1")
	if err != nil {
		t.Fatalf("DecisionModel() error = %v", err)
	}
	if got != model {
		t.Fatalf("DecisionModel() returned a different model")
	}
}

func TestCustomProvider_DecisionModel_FallsBackToFallbackProvider(t *testing.T) {
	fallbackModel := &mockDecisionModel{provider: "fallback", modelID: "jev-2"}
	fallback := &decisionModelProvider{
		MockProvider: &testutil.MockProvider{ProviderName: "fallback"},
		models:       map[string]provider.DecisionModel{"jev-2": fallbackModel},
	}
	p := NewCustomProvider(CustomProviderOptions{Fallback: fallback})

	ep, ok := p.(provider.DecisionModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement DecisionModelProvider")
	}
	got, err := ep.DecisionModel("jev-2")
	if err != nil {
		t.Fatalf("DecisionModel() error = %v", err)
	}
	if got != fallbackModel {
		t.Fatalf("DecisionModel() returned a different model")
	}
}

func TestCustomProvider_DecisionModel_NoSuchModel(t *testing.T) {
	p := NewCustomProvider(CustomProviderOptions{})
	ep, ok := p.(provider.DecisionModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement DecisionModelProvider")
	}
	if _, err := ep.DecisionModel("missing"); err == nil {
		t.Fatalf("expected error for missing decision model")
	}
}

func TestRegistry_ResolveDecisionModel(t *testing.T) {
	model := &mockDecisionModel{provider: "custom", modelID: "jev-1"}
	p := &decisionModelProvider{
		MockProvider: &testutil.MockProvider{ProviderName: "custom"},
		models:       map[string]provider.DecisionModel{"jev-1": model},
	}

	r := NewRegistry()
	r.RegisterProvider("custom", p)

	got, err := r.ResolveDecisionModel("custom:jev-1")
	if err != nil {
		t.Fatalf("ResolveDecisionModel() error = %v", err)
	}
	if got != model {
		t.Fatalf("ResolveDecisionModel() returned a different model")
	}
}

func TestRegistry_ResolveDecisionModel_ProviderWithoutSupport(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("plain", &testutil.MockProvider{ProviderName: "plain"})

	_, err := r.ResolveDecisionModel("plain:jev-1")
	var target *NoSuchProviderError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*NoSuchProviderError); ok {
		target = e
	}
	if target == nil || target.ModelType != "decisionModel" {
		t.Fatalf("err = %v (%T), want NoSuchProviderError{ModelType: decisionModel}", err, err)
	}
}

func TestRegistry_ResolveDecisionModel_UnknownProvider(t *testing.T) {
	r := NewRegistry()
	_, err := r.ResolveDecisionModel("missing:jev-1")
	if err == nil {
		t.Fatalf("expected error")
	}
}

// Deprecated alias coverage, ported from TS deprecated-aliases.test.ts.

// onlyEvaluationModel implements just the legacy contract.
type onlyEvaluationModel struct{}

func (onlyEvaluationModel) SpecificationVersion() string     { return "v4" }
func (onlyEvaluationModel) Provider() string                 { return "legacy" }
func (onlyEvaluationModel) ModelID() string                  { return "legacy-model" }
func (onlyEvaluationModel) SupportedQuestionTypes() []string { return []string{"choice"} }
func (onlyEvaluationModel) DoEvaluate(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return &provider.DecisionResult{Warnings: nil}, nil
}

//nolint:staticcheck // exercises the deprecated EvaluationModelProvider
type legacyEvalProvider struct {
	*testutil.MockProvider
	model provider.EvaluationModel
}

func (p *legacyEvalProvider) EvaluationModel(string) (provider.EvaluationModel, error) {
	return p.model, nil
}

func TestRegistry_ResolveDecisionAndEvaluationModel_LegacyProvider(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("legacy", &legacyEvalProvider{MockProvider: &testutil.MockProvider{ProviderName: "legacy"}, model: onlyEvaluationModel{}})

	decision, err := r.ResolveDecisionModel("legacy:model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decision.DoDecide(context.Background(), provider.DecisionCallOptions{}); err != nil {
		t.Fatal(err)
	}
	evaluation, err := r.ResolveEvaluationModel("legacy:model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evaluation.DoEvaluate(context.Background(), provider.DecisionCallOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCustomProvider_EvaluationModels_DeprecatedMapAndPrecedence(t *testing.T) {
	legacy := onlyEvaluationModel{}
	p := NewCustomProvider(CustomProviderOptions{EvaluationModels: map[string]provider.EvaluationModel{"alias": legacy}})
	dm, err := p.(provider.DecisionModelProvider).DecisionModel("alias")
	if err != nil || dm.ModelID() != "legacy-model" {
		t.Fatalf("decision via deprecated map: %v %v", dm, err)
	}
	em, err := p.(provider.EvaluationModelProvider).EvaluationModel("alias")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := em.DoEvaluate(context.Background(), provider.DecisionCallOptions{}); err != nil {
		t.Fatal(err)
	}

	newModel := &mockDecisionModel{provider: "new", modelID: "new-model"}
	p = NewCustomProvider(CustomProviderOptions{
		DecisionModels:   map[string]provider.DecisionModel{"alias": newModel},
		EvaluationModels: map[string]provider.EvaluationModel{"alias": legacy},
	})
	got, err := p.(provider.DecisionModelProvider).DecisionModel("alias")
	if err != nil || got != provider.DecisionModel(newModel) {
		t.Fatalf("expected the DecisionModels entry to win, got %v %v", got, err)
	}
}

func TestCustomProvider_DecisionModel_LegacyFallbackProvider(t *testing.T) {
	p := NewCustomProvider(CustomProviderOptions{
		Fallback: &legacyEvalProvider{MockProvider: &testutil.MockProvider{ProviderName: "legacy"}, model: onlyEvaluationModel{}},
	})
	dm, err := p.(provider.DecisionModelProvider).DecisionModel("anything")
	if err != nil || dm.ModelID() != "legacy-model" {
		t.Fatalf("got %v %v", dm, err)
	}
}
