package fireworks

import (
	"context"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS loadApiKey throws LoadAPIKeyError before the fetch; the embedding
// handler wrapped it in a status-0 ProviderError.
func TestR92provbEmbeddingMissingAPIKey(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "")
	m, _ := New(Config{BaseURL: "http://127.0.0.1:1"}).EmbeddingModel("nomic-ai/nomic-embed-text-v1.5")
	_, err := m.DoEmbed(context.Background(), "x", nil)
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("err = %T %v, want *LoadAPIKeyError", err, err)
	}
}
