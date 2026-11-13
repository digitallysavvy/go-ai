package fireworks

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

// TestR98provbNullErrorIsOffSchema: TS fireworksErrorSchema's error is
// z.union([z.string(), z.object(...)]), so {"error": null} fails it and the
// message is the status text. Go decoded null into the string branch and
// reported an empty message.
func TestR98provbNullErrorIsOffSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":null}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("m")
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Bad Request" {
		t.Fatalf("err = %v, want the status text", err)
	}
}
