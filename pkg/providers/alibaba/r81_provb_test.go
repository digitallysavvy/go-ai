package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r81provbAlibabaGenerate(t *testing.T, body string) (*types.GenerateResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	return m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
}

// TS convertAlibabaUsage(undefined | null) is createNullLanguageModelUsage:
// a response without usage reports unknown counts, not zero tokens
// (alibaba "should omit a reasoning-only response from a follow-up request",
// replayed with results from a recording at e64ed82f27).
func TestR81provbAlibabaMissingUsageIsNullUsage(t *testing.T) {
	for name, usage := range map[string]string{"absent": "", "null": `,"usage":null`} {
		t.Run(name, func(t *testing.T) {
			res, err := r81provbAlibabaGenerate(t, `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]`+usage+`}`)
			if err != nil {
				t.Fatal(err)
			}
			if u := res.Usage; u.InputTokens != nil || u.OutputTokens != nil || u.TotalTokens != nil || u.InputDetails != nil || u.OutputDetails != nil || u.Raw != nil {
				t.Fatalf("usage = %+v, want the null usage", u)
			}
		})
	}
	res, err := r81provbAlibabaGenerate(t, `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 3 || *res.Usage.OutputTokens != 2 {
		t.Fatalf("reported usage = %+v", res.Usage)
	}
}

// TS doGenerate reads response.choices[0].message without a guard, so an
// empty choices array is a TypeError (Go returned an empty result).
func TestR81provbAlibabaEmptyChoicesIsError(t *testing.T) {
	res, err := r81provbAlibabaGenerate(t, `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)
	if err == nil || err.Error() != "Cannot read properties of undefined (reading 'message')" {
		t.Fatalf("err = %v, result = %+v", err, res)
	}
}
