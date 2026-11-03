package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r86provaBOMErrorBody is an Anthropic error body behind a UTF-8 BOM.
const r86provaBOMErrorBody = "\xEF\xBB\xBF{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"boom\"}}"

// TS createJsonErrorResponseHandler parses response.text(), which drops a
// leading BOM: a streamed request's error body (handed over undecoded) gives
// the API error message, like the generate path.
func TestR86provaAnthropicErrorBodyBOM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(r86provaBOMErrorBody))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	_, genErr := m.DoGenerate(context.Background(), opts)
	_, streamErr := m.DoStream(context.Background(), opts)
	for name, err := range map[string]error{"generate": genErr, "stream": streamErr} {
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "boom" || pe.ErrorCode != "invalid_request_error" {
			t.Fatalf("%s: %#v", name, err)
		}
		if pe.ResponseBody != r86provaBOMErrorBody[3:] {
			t.Fatalf("%s: ResponseBody = %q", name, pe.ResponseBody)
		}
	}
}
