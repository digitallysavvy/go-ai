package registry

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type mockEvaluationModel struct {
	provider string
	modelID  string
}

func (m *mockEvaluationModel) SpecificationVersion() string { return "v4" }
func (m *mockEvaluationModel) Provider() string             { return m.provider }
func (m *mockEvaluationModel) ModelID() string              { return m.modelID }
func (m *mockEvaluationModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}
func (m *mockEvaluationModel) DoEvaluate(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
	return &provider.EvaluationResult{}, nil
}

// evaluationModelProvider wraps testutil.MockProvider with
// provider.EvaluationModelProvider support for registry/custom-provider tests.
type evaluationModelProvider struct {
	*testutil.MockProvider
	models map[string]provider.EvaluationModel
}

func (p *evaluationModelProvider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	if m, ok := p.models[modelID]; ok {
		return m, nil
	}
	return nil, noSuchModel(modelID, "evaluationModel")
}

func TestCustomProvider_EvaluationModel_UsesRegisteredModel(t *testing.T) {
	model := &mockEvaluationModel{provider: "custom", modelID: "jev-1"}
	p := NewCustomProvider(CustomProviderOptions{
		EvaluationModels: map[string]provider.EvaluationModel{"jev-1": model},
	})

	ep, ok := p.(provider.EvaluationModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement EvaluationModelProvider")
	}
	got, err := ep.EvaluationModel("jev-1")
	if err != nil {
		t.Fatalf("EvaluationModel() error = %v", err)
	}
	if got != model {
		t.Fatalf("EvaluationModel() returned a different model")
	}
}

func TestCustomProvider_EvaluationModel_FallsBackToFallbackProvider(t *testing.T) {
	fallbackModel := &mockEvaluationModel{provider: "fallback", modelID: "jev-2"}
	fallback := &evaluationModelProvider{
		MockProvider: &testutil.MockProvider{ProviderName: "fallback"},
		models:       map[string]provider.EvaluationModel{"jev-2": fallbackModel},
	}
	p := NewCustomProvider(CustomProviderOptions{Fallback: fallback})

	ep, ok := p.(provider.EvaluationModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement EvaluationModelProvider")
	}
	got, err := ep.EvaluationModel("jev-2")
	if err != nil {
		t.Fatalf("EvaluationModel() error = %v", err)
	}
	if got != fallbackModel {
		t.Fatalf("EvaluationModel() returned a different model")
	}
}

func TestCustomProvider_EvaluationModel_NoSuchModel(t *testing.T) {
	p := NewCustomProvider(CustomProviderOptions{})
	ep, ok := p.(provider.EvaluationModelProvider)
	if !ok {
		t.Fatalf("custom provider does not implement EvaluationModelProvider")
	}
	if _, err := ep.EvaluationModel("missing"); err == nil {
		t.Fatalf("expected error for missing evaluation model")
	}
}

func TestRegistry_ResolveEvaluationModel(t *testing.T) {
	model := &mockEvaluationModel{provider: "custom", modelID: "jev-1"}
	p := &evaluationModelProvider{
		MockProvider: &testutil.MockProvider{ProviderName: "custom"},
		models:       map[string]provider.EvaluationModel{"jev-1": model},
	}

	r := NewRegistry()
	r.RegisterProvider("custom", p)

	got, err := r.ResolveEvaluationModel("custom:jev-1")
	if err != nil {
		t.Fatalf("ResolveEvaluationModel() error = %v", err)
	}
	if got != model {
		t.Fatalf("ResolveEvaluationModel() returned a different model")
	}
}

func TestRegistry_ResolveEvaluationModel_ProviderWithoutSupport(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("plain", &testutil.MockProvider{ProviderName: "plain"})

	_, err := r.ResolveEvaluationModel("plain:jev-1")
	var target *NoSuchProviderError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*NoSuchProviderError); ok {
		target = e
	}
	if target == nil || target.ModelType != "evaluationModel" {
		t.Fatalf("err = %v (%T), want NoSuchProviderError{ModelType: evaluationModel}", err, err)
	}
}

func TestRegistry_ResolveEvaluationModel_UnknownProvider(t *testing.T) {
	r := NewRegistry()
	_, err := r.ResolveEvaluationModel("missing:jev-1")
	if err == nil {
		t.Fatalf("expected error")
	}
}
