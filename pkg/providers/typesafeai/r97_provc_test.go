package typesafeai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provcFractionalUsageTokens: typesafeDecisionResponseSchema declares
// usage.input_tokens / output_tokens as z.number().nullish(), so TS accepts
// any finite count and returns it. The central decoder already truncated a
// fraction (12.5), but a count past the int range (1e300) failed the whole
// call. The count is now read as a number, truncated toward zero and
// clamped to the int range.
func TestR97provcFractionalUsageTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.14.0","answers":{"q":{"type":"noul","noul":0.7}},"usage":{"input_tokens":12.5,"output_tokens":1e300}}`))
	}))
	defer srv.Close()
	m := NewDecisionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "jev-1.14.0")
	res, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "state",
		Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "is it?"}},
	})
	if err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	if res.Usage == nil || res.Usage.InputTokens == nil || *res.Usage.InputTokens != 12 {
		t.Fatalf("usage = %+v, want inputTokens 12", res.Usage)
	}
	if res.Usage.OutputTokens == nil || *res.Usage.OutputTokens <= 0 {
		t.Fatalf("usage = %+v, want a clamped positive outputTokens", res.Usage)
	}
	if res.Answers["q"].Probability == nil || *res.Answers["q"].Probability != 0.7 {
		t.Fatalf("answers = %+v", res.Answers)
	}
}
