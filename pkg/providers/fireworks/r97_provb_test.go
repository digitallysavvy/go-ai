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

// TestR97provbErrorCodeBeyondDoubleRange: JSON.parse reads code 1e400 as
// Infinity, which z.number() rejects, so the body fails fireworksErrorSchema
// and the message is the status text (Go accepted the code and used the
// envelope message). An underflowing code (0) stays valid.
func TestR97provbErrorCodeBeyondDoubleRange(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"code":1e400,"message":"boom"}}`:  "Bad Request",
		`{"error":{"code":1e-400,"message":"boom"}}`: "boom",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(body))
		}))
		model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("m")
		_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		server.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want {
			t.Errorf("%s: err = %v, want message %q", body, err, want)
		}
	}
}
