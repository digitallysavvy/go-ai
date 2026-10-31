package google

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's `provider.decisionModel = (modelId) => new
// DecisionLanguageModel({ model: createChatModel(modelId), provider:
// providerName.replace(/\.generative-ai$/, '') + '.decision' })` in
// google-provider.ts.

func TestProvider_DecisionModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.DecisionModel(ModelGemini20Flash)
	if err != nil {
		t.Fatalf("DecisionModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "google.decision" {
		t.Fatalf("Provider = %q, want google.decision", model.Provider())
	}
	if model.ModelID() != ModelGemini20Flash {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
}

func TestProvider_DecisionModel_DoDecide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"{\"q0\":\"c0\"}"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.DecisionModel(ModelGemini20Flash)
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
	answer := result.Answers["topic"]
	if answer.Type != "choice" || answer.Choice != "billing" {
		t.Fatalf("answer = %+v", answer)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 10 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
