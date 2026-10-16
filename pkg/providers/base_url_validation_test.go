package providers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
)

// TS createOpenAI / createReplicate run validateBaseURL: a whitespace-only
// baseURL is an InvalidArgumentError "baseURL must be a non-empty string.".
// Go keeps New panic-free (v0.5.2 accepted these values) and reports the
// error on every request instead, like the request-time LoadAPIKeyError.
func TestWhitespaceOnlyBaseURLFailsAtRequestTime(t *testing.T) {
	check := func(name string, mk func() (provider.LanguageModel, error)) {
		t.Helper()
		var m provider.LanguageModel
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: New panicked: %v", name, r)
				}
			}()
			var err error
			if m, err = mk(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}()
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Field != "baseURL" || iae.Message != "baseURL must be a non-empty string." {
			t.Fatalf("%s: err = %v, want InvalidArgumentError baseURL must be a non-empty string.", name, err)
		}
	}
	check("openai", func() (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k", BaseURL: "   "}).LanguageModel("gpt-4o")
	})
	check("replicate", func() (provider.LanguageModel, error) {
		return replicate.New(replicate.Config{APIKey: "k", BaseURL: "  \t"}).LanguageModel("owner/model")
	})
	t.Setenv("OPENAI_BASE_URL", "   ")
	check("openai env", func() (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k"}).LanguageModel("gpt-4o")
	})
}
