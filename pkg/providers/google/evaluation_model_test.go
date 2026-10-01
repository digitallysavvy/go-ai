package google

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's `provider.evaluationModel = (modelId) => new
// EvaluationLanguageModel({ model: createChatModel(modelId), provider:
// providerName.replace(/\.generative-ai$/, '') + '.evaluation' })` in
// google-provider.ts.

func TestProvider_EvaluationModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.EvaluationModel(ModelGemini20Flash)
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "google.evaluation" {
		t.Fatalf("Provider = %q, want google.evaluation", model.Provider())
	}
	if model.ModelID() != ModelGemini20Flash {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
}

func TestProvider_EvaluationModel_DoEvaluate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"{\"q0\":\"c0\"}"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.EvaluationModel(ModelGemini20Flash)
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
	answer := result.Answers["topic"]
	if answer.Type != "choice" || answer.Choice != "billing" {
		t.Fatalf("answer = %+v", answer)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 10 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
