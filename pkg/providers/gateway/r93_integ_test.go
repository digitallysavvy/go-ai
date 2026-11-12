package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integDecisionFractionalNumbersDecode: TS
// gatewayDecisionResponseSchema reads rounding and usage as z.number(), so
// a fractional value is a valid response. The shared client truncates it
// into the integer v0.5.2 fields, so the call succeeds.
func TestR93integDecisionFractionalNumbersDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers": {}, "rounding": {"probabilityDecimals": 2.5, "scoreDecimals": 1}, "usage": {"inputTokens": 10.5, "outputTokens": 3.2}}`))
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.DecisionModel("typesafe-ai/jev")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "s", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}},
	})
	if err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 10 || *res.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.Rounding == nil || *res.Rounding.ProbabilityDecimals != 2 || *res.Rounding.ScoreDecimals != 1 {
		t.Fatalf("rounding = %+v", res.Rounding)
	}
}
