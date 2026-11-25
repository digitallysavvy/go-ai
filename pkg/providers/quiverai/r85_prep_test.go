package quiverai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r85prepQuiverCall(t *testing.T, reasoning types.ReasoningLevel, providerOptions map[string]interface{}) (map[string]interface{}, []types.Warning) {
	t.Helper()
	var requestBody map[string]interface{}
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&requestBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(quiverResponse(nil, "", nil))
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          quiverPrompt("Create an icon."),
		Reasoning:       reasoningPtr(reasoning),
		ProviderOptions: providerOptions,
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	return requestBody, result.Warnings
}

// TS 61a315e333: "max" is not a QuiverAI level and rounds down to "xhigh".
func TestR85prepQuiverAIMaxRoundsToXHigh(t *testing.T) {
	body, warnings := r85prepQuiverCall(t, types.ReasoningMax, nil)
	reasoning, _ := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("reasoning = %v, want effort xhigh", body["reasoning"])
	}
	want := `reasoning "max" is not directly supported by this model. mapped to effort "xhigh".`
	found := false
	for _, w := range warnings {
		if w.Type == "compatibility" && w.Feature == "reasoning" && w.Details == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want %q", warnings, want)
	}
}

// A provider-option effort wins and the top-level value is not mapped.
func TestR85prepQuiverAIProviderEffortWins(t *testing.T) {
	body, warnings := r85prepQuiverCall(t, types.ReasoningNone, map[string]interface{}{
		"quiverai": map[string]interface{}{"reasoningEffort": "high"},
	})
	reasoning, _ := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %v, want effort high", body["reasoning"])
	}
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("unexpected reasoning warning %+v", w)
		}
	}
}
