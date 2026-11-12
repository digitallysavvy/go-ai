package xai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Typed-option ruling: the deprecated `xai` namespace given as a Go-typed
// literal is read like its JSON form (user forwarded, deprecation warning).
func TestR92provgDeprecatedXaiTypedLiteral(t *testing.T) {
	srv, got := r91provgServer(t)
	p, _ := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: srv.URL})
	m, _ := p.LanguageModel(ModelGrok41FastReasoning)
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{"xai": map[string]string{"user": "u1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, body := got(); !strings.Contains(body, `"user":"u1"`) {
		t.Fatalf("body = %s", body)
	}
	deprecated := false
	for _, w := range res.Warnings {
		deprecated = deprecated || w.Type == "deprecated"
	}
	if !deprecated {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}
