package deepinfra

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS loadApiKey throws from getHeaders before the fetch, so a missing key is
// the LoadAPIKeyError itself, not an APICallError (cr-r92-provb-to-provg-tools),
// for generation and for editing.
func TestR92toolsDeepInfraImageMissingKeyUnwrapped(t *testing.T) {
	t.Setenv("DEEPINFRA_API_KEY", "")
	m, _ := New(Config{BaseURL: "http://127.0.0.1:1/v1/openai"}).ImageModel("black-forest-labs/FLUX-1-schnell")
	for name, opts := range map[string]*provider.ImageGenerateOptions{
		"generate": {Prompt: "x"},
		"edit":     {Prompt: "x", Files: []provider.ImageFile{{Type: "file", Data: []byte{1}, MediaType: "image/png"}}},
	} {
		_, err := m.DoGenerate(context.Background(), opts)
		var keyErr *providererrors.LoadAPIKeyError
		var perr *providererrors.ProviderError
		if !errors.As(err, &keyErr) || errors.As(err, &perr) {
			t.Fatalf("%s: err = %T %v, want an unwrapped LoadAPIKeyError", name, err, err)
		}
	}
}
