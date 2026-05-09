package voyage

import "testing"

func TestProviderFactories(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	em, err := p.EmbeddingModel(ModelVoyage3Lite)
	if err != nil || em == nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	if em.Provider() != "voyage.embedding" {
		t.Fatalf("embedding provider = %q, want voyage.embedding", em.Provider())
	}
	alias, err := p.TextEmbeddingModel(ModelVoyage3Lite)
	if err != nil || alias == nil {
		t.Fatalf("TextEmbeddingModel error = %v", err)
	}
	rm, err := p.RerankingModel(ModelRerank2)
	if err != nil || rm == nil {
		t.Fatalf("RerankingModel error = %v", err)
	}
	if rm.Provider() != "voyage.reranking" {
		t.Fatalf("reranking provider = %q, want voyage.reranking", rm.Provider())
	}
}
