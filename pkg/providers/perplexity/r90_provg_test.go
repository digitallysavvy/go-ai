package perplexity

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provgEmbeddingProviderID: TS createPerplexity builds the embedding
// model with provider 'perplexity.embedding' (perplexity-provider.ts), the
// value TooManyEmbeddingValuesForCallError and telemetry report; payloads
// serialized under the v0.5 "perplexity" key still restore.
func TestR90provgEmbeddingProviderID(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m, _ := p.EmbeddingModel("pplx-embed-v1-4b")
	if m.Provider() != "perplexity.embedding" {
		t.Fatalf("Provider() = %q, want perplexity.embedding", m.Provider())
	}
	_, err := m.DoEmbedMany(context.Background(), make([]string, 513), nil)
	var tooMany *providererrors.TooManyEmbeddingValuesForCallError
	if !errors.As(err, &tooMany) || tooMany.Provider != "perplexity.embedding" {
		t.Fatalf("err = %v, want TooManyEmbeddingValuesForCallError from perplexity.embedding", err)
	}
	for _, key := range []string{"perplexity", "perplexity.embedding"} {
		restored, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: key, ModelID: "pplx-embed-v1-4b"})
		if err != nil || restored.Provider() != "perplexity.embedding" {
			t.Fatalf("restore %q: %v %v", key, restored, err)
		}
	}
}
