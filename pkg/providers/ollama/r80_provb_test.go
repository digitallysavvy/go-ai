package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertOpenAICompatibleChatUsage(undefined) is the null usage: a
// response without a usage object has unknown token counts, not zero
// (openai-compatible "should process metadata from complete response").
func TestR80provbOllamaMissingUsageIsNullUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		body     string
		wantNull bool
	}{
		"absent": {`{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`, true},
		"null":   {`{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":null}`, true},
		"empty":  {`{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m := NewLanguageModel(New(Config{BaseURL: srv.URL}), "m")
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Usage.InputTokens == nil && res.Usage.OutputTokens == nil && res.Usage.TotalTokens == nil; got != tc.wantNull {
				t.Fatalf("usage = %+v, want null usage %v", res.Usage, tc.wantNull)
			}
			if !tc.wantNull && (*res.Usage.InputTokens != 0 || *res.Usage.OutputTokens != 0) {
				t.Fatalf("usage = %+v, want zero counts", res.Usage)
			}
			if res.Text != "hi" || res.FinishReason != types.FinishReasonStop {
				t.Fatalf("text %q finish %q", res.Text, res.FinishReason)
			}
		})
	}
}
