package anthropic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's `provider.decisionModel = (modelId) => new
// DecisionLanguageModel({ model: createChatModel(modelId), provider:
// providerName.replace(/\.messages$/, '') + '.decision' })` in
// anthropic-provider.ts.

func TestProvider_DecisionModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.DecisionModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("DecisionModel: %v", err)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", model.SpecificationVersion())
	}
	if model.Provider() != "anthropic.decision" {
		t.Fatalf("Provider = %q, want anthropic.decision", model.Provider())
	}
	if model.ModelID() != ClaudeSonnet4_5 {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
}

func TestProvider_DecisionModel_DoDecide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "{\"q0\":\"c0\"}"}],
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.DecisionModel(ClaudeSonnet4_5)
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
