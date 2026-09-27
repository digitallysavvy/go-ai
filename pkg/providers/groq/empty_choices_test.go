package groq

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

// Ported (behaviorally) from ai/packages/groq/src/groq-chat-language-model.ts's
// "Response did not contain any choices." InvalidResponseDataError (c0fd23b).

func TestGroq_EmptyChoicesReturnsInvalidResponseDataError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[]}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !providererrors.IsInvalidResponseDataError(err) {
		t.Fatalf("error = %T %v, want InvalidResponseDataError", err, err)
	}
	var invalidErr *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalidErr) {
		t.Fatal("errors.As failed")
	}
	if invalidErr.Message != "Response did not contain any choices." {
		t.Fatalf("Message = %q", invalidErr.Message)
	}
}
