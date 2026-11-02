package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r81provbCompletionGenerate(t *testing.T, cfg Config, body string) (*types.GenerateResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	cfg.APIKey, cfg.BaseURL = "k", srv.URL
	m, err := New(cfg).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	return m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
}

// TS completion doGenerate reads response.choices[0] without a guard: the
// first property read is a TypeError (openai choice.logprobs,
// openai-compatible choice.text). Go returned an empty result.
func TestR81provbCompletionEmptyChoicesIsError(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"openai", Config{}, "Cannot read properties of undefined (reading 'logprobs')"},
		{"openai-compatible", Config{OpenAICompatible: true, Name: "acme"}, "Cannot read properties of undefined (reading 'text')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r81provbCompletionGenerate(t, tc.cfg, `{"id":"c","object":"text_completion","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, result = %+v", err, res)
			}
		})
	}
}

// TS openai-compatible completion pushes the text part only when the text is
// non-empty; TS openai always pushes it.
func TestR81provbCompletionEmptyTextContent(t *testing.T) {
	body := `{"id":"c","object":"text_completion","created":1,"model":"m","choices":[{"index":0,"text":"","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
	res, err := r81provbCompletionGenerate(t, Config{OpenAICompatible: true, Name: "acme"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 0 {
		t.Fatalf("openai-compatible content = %#v, want none", res.Content)
	}
	res, err = r81provbCompletionGenerate(t, Config{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("openai content = %#v, want one empty text part", res.Content)
	}
}
