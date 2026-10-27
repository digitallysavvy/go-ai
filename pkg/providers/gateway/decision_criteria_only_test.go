package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23470: criteria-only questions are forwarded without instructions.
func TestEncodeGatewayDecisionQuestions_CriteriaOnly(t *testing.T) {
	out := encodeGatewayDecisionQuestions(map[string]provider.DecisionQuestion{
		"correct": {Type: "boolean", Criteria: map[string]interface{}{"true": "Factually correct"}},
		"quality": {Type: "score", Criteria: []interface{}{"Poor", "Great"}},
	})
	for id, wire := range out {
		if _, ok := wire.(map[string]interface{})["instructions"]; ok {
			t.Errorf("%s: instructions must be omitted: %#v", id, wire)
		}
	}
}

// TS #23590: maxOutputTokens and reasoning are forwarded when set only.
func TestGatewayDecisionModel_ForwardsMaxOutputTokensAndReasoning(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers": {}}`))
	}))
	defer server.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.DecisionModel("m")
	q := map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}
	_, _ = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: q})
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatal("reasoning must be omitted when unset")
	}
	if _, ok := gotBody["maxOutputTokens"]; ok {
		t.Fatal("maxOutputTokens must be omitted when unset")
	}
	n, level := 512, types.ReasoningDefault
	_, _ = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: q, MaxOutputTokens: &n, Reasoning: &level})
	if gotBody["reasoning"] != "provider-default" || gotBody["maxOutputTokens"] != float64(512) {
		t.Fatalf("body = %v", gotBody)
	}
}
