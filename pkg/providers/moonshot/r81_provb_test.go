package moonshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate reads responseBody.choices[0].message without a guard, so an
// empty choices array is a TypeError (Go returned an empty result).
func TestR81provbMoonshotEmptyChoicesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err == nil || err.Error() != "Cannot read properties of undefined (reading 'message')" {
		t.Fatalf("err = %v, result = %+v", err, res)
	}
}
