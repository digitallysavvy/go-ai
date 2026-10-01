package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's `provider.evaluationModel = (modelId) => new
// EvaluationLanguageModel({ model: createResponsesModel(modelId), provider:
// providerName + '.evaluation' })` in openai-provider.ts.

func TestProvider_EvaluationModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.EvaluationModel("gpt-4o")
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "openai.evaluation" {
		t.Fatalf("Provider = %q, want openai.evaluation", model.Provider())
	}
	if model.ModelID() != "gpt-4o" {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
}

func TestProvider_EvaluationModel_DoEvaluate(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResponsesResponse("resp_1", `{"q0":"c0"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.EvaluationModel("gpt-4o")
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}

	result, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State: "The customer wants a refund.",
		Questions: map[string]provider.EvaluationQuestion{
			"topic": {
				Type:         "choice",
				Instructions: "What is the topic?",
				Criteria:     map[string]interface{}{"billing": "Billing related"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q, want /responses (evaluationModel should use the Responses API)", gotPath)
	}
	answer := result.Answers["topic"]
	if answer.Type != "choice" || answer.Choice != "billing" {
		t.Fatalf("answer = %+v", answer)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
