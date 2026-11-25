package moonshot

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

// PB-20 (provb2a-02): prompt checks are TS InvalidPromptError.
func TestR91provbMoonshotInvalidPrompt(t *testing.T) {
	_, _, err := convertToMoonshotChatMessages("kimi-k2.5", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}},
		ProviderOptions: map[string]interface{}{"moonshotai": map[string]interface{}{"partial": true}}}}, "")
	var ip *providererrors.InvalidPromptError
	if !errors.As(err, &ip) || err.Error() != "Invalid prompt: Moonshot AI Partial Mode requires `partial: true` on an assistant message." {
		t.Fatalf("err = %v", err)
	}
}

// PB-21 (provb2a-03) and PB-22 (provb2a-04): a non-schema error body is the
// status text (TS createJsonErrorResponseHandler), and a cancelled call is
// the context error unchanged (TS handleFetchError).
func TestR91provbMoonshotErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
	}))
	defer srv.Close()
	lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
	_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Too Many Requests" {
		t.Fatalf("err = %#v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if !errors.Is(err, context.Canceled) || errors.As(err, &pe) {
		t.Fatalf("cancel err = %#v", err)
	}
}
