package typesafeai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integFractionalUsageDecodes: TS usage counts are
// z.number().nullish(), so a fractional count is a valid response. The
// shared client truncates it into the integer DecisionUsage fields (v0.5.2
// EvaluationUsage), so the call succeeds (provc r93 observation, checked).
func TestR93integFractionalUsageDecodes(t *testing.T) {
	body := strings.Replace(nativeResponseJSON, `"usage": {"input_tokens": 471, "output_tokens": 71}`, `"usage": {"input_tokens": 471.5, "output_tokens": 71.9}`, 1)
	if body == nativeResponseJSON {
		t.Fatal("fixture usage not found")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).DecisionModel("jev-latest")
	res, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{State: "test", Questions: nativeQuestions()})
	if err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	if res.Usage == nil || res.Usage.InputTokens == nil || *res.Usage.InputTokens != 471 || res.Usage.OutputTokens == nil || *res.Usage.OutputTokens != 71 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}
