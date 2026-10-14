package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's `provider.decisionModel = (modelId) => new
// DecisionLanguageModel({ model: createResponsesModel(modelId), provider:
// providerName + '.decision' })` in openai-provider.ts.

func TestProvider_DecisionModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.DecisionModel("gpt-4o")
	if err != nil {
		t.Fatalf("DecisionModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "openai.decision" {
		t.Fatalf("Provider = %q, want openai.decision", model.Provider())
	}
	if model.ModelID() != "gpt-4o" {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
}

func TestProvider_DecisionModel_DoDecide(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResponsesResponse("resp_1", `{"q0":"c0"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.DecisionModel("gpt-4o")
	if err != nil {
		t.Fatalf("DecisionModel: %v", err)
	}

	result, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "The customer wants a refund.",
		Questions: map[string]provider.DecisionQuestion{
			"topic": {
				Type:         "choice",
				Instructions: "What is the topic?",
				Criteria:     map[string]interface{}{"billing": "Billing related"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q, want /responses (decisionModel should use the Responses API)", gotPath)
	}
	answer := result.Answers["topic"]
	if answer.Type != "choice" || answer.Choice != "billing" {
		t.Fatalf("answer = %+v", answer)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
