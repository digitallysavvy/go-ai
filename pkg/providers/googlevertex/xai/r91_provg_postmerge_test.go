package xai

// Post-merge test (provg, round 91, added by the integrator after the provb
// merge): provb2a-11 makes providerutils mergeProviderOptions read typed
// literals through NormalizeOptionsJSON. Uses r91provgServer from
// r91_provg_test.go.

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Item 7 (provb2a-11 dependency): Go-typed option literals through the
// shared mergeProviderOptions helper reach the body like their JSON form.
func TestR91provgPostMergeTypedOptionLiterals(t *testing.T) {
	srv, got := r91provgServer(t)
	p, _ := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: srv.URL})
	m, _ := p.LanguageModel(ModelGrok41FastReasoning)
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{
		"googleVertex": map[string]string{"user": "u1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, _, body := got(); !strings.Contains(body, `"user":"u1"`) {
		t.Fatalf("body = %s", body)
	}
}
