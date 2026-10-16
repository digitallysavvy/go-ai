package googlevertex

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS validates providerOptions before comparing the value count, so invalid
// options win over TooManyEmbeddingValuesForCallError.
func TestEmbeddingValidatesProviderOptionsBeforeValueCount(t *testing.T) {
	m := NewEmbeddingModel(newVertexTestProvider(t, "http://example.invalid"), "gemini-embedding-2")
	_, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"googleVertex": map[string]interface{}{"taskType": "NOPE"}},
	})
	var iae *providererrors.InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("err = %#v, want InvalidArgumentError", err)
	}
}
