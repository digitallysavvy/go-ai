package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
)

// TS createOpenAI / createReplicate run validateBaseURL: a whitespace-only
// baseURL throws "baseURL must be a non-empty string.".
func TestWhitespaceOnlyBaseURLPanics(t *testing.T) {
	for name, build := range map[string]func(){
		"openai":    func() { openai.New(openai.Config{APIKey: "k", BaseURL: "   "}) },
		"replicate": func() { replicate.New(replicate.Config{APIKey: "k", BaseURL: "  \t"}) },
	} {
		func() {
			defer func() {
				r := recover()
				err, ok := r.(error)
				if !ok || err.Error() != "baseURL must be a non-empty string." {
					t.Errorf("%s: recovered %v, want the validateBaseURL error", name, r)
				}
			}()
			build()
		}()
	}
	t.Setenv("OPENAI_BASE_URL", "   ")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("OPENAI_BASE_URL whitespace must also be rejected")
			}
		}()
		openai.New(openai.Config{APIKey: "k"})
	}()
}
