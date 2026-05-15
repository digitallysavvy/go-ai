package voyage

import (
	"context"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVoyageProvider_AliasesAndUnsupported(t *testing.T) {
	t.Parallel()

	_ = os.Setenv("VOYAGE_API_KEY", "env-key")
	t.Cleanup(func() { _ = os.Unsetenv("VOYAGE_API_KEY") })

	p := CreateVoyage(Config{})
	if p.Name() != "voyage" {
		t.Fatalf("Name() = %q, want voyage", p.Name())
	}

	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel expected unsupported error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel expected unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}

	em1, err := p.Embedding("voyage-3-lite")
	if err != nil || em1 == nil {
		t.Fatalf("Embedding() error = %v", err)
	}
	em2, err := p.TextEmbedding("voyage-3-lite")
	if err != nil || em2 == nil {
		t.Fatalf("TextEmbedding() error = %v", err)
	}
	rr, err := p.Reranking("rerank-2")
	if err != nil || rr == nil {
		t.Fatalf("Reranking() error = %v", err)
	}

	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("EmbeddingModel(\"\") expected validation error")
	}
	if _, err := p.RerankingModel(""); err == nil {
		t.Fatal("RerankingModel(\"\") expected validation error")
	}
}

func TestVoyageEmbeddingModel_MetadataAndSingleEmbed(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	m := NewEmbeddingModel(p, ModelVoyage3Lite)

	if m.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.ModelID() != ModelVoyage3Lite {
		t.Fatalf("ModelID() = %q", m.ModelID())
	}
	if !m.SupportsParallelCalls() {
		t.Fatal("SupportsParallelCalls() = false, want true")
	}
}

func TestVoyageHelpersAndRerankingMetadata(t *testing.T) {
	t.Parallel()

	if got := optsHeaders(nil); got != nil {
		t.Fatalf("optsHeaders(nil) = %#v, want nil", got)
	}
	vo := voyageProviderOptions(&provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"voyage": map[string]interface{}{"outputDimension": 256},
		},
	})
	if vo == nil || vo["outputDimension"] != 256 {
		t.Fatalf("voyageProviderOptions parse failed: %#v", vo)
	}

	docs, warnings := toVoyageDocuments([]map[string]interface{}{
		{"title": "a"},
	})
	if len(docs) != 1 || len(warnings) != 1 {
		t.Fatalf("toVoyageDocuments object conversion failed: docs=%v warnings=%v", docs, warnings)
	}
	noneDocs, noneWarnings := toVoyageDocuments(123)
	if len(noneDocs) != 0 || len(noneWarnings) != 0 {
		t.Fatalf("unexpected conversion for unsupported doc type: docs=%v warnings=%v", noneDocs, noneWarnings)
	}

	m := NewRerankingModel(New(Config{APIKey: "k", BaseURL: "https://example.test"}), ModelRerank2)
	if m.SpecificationVersion() != "v4" || m.ModelID() != ModelRerank2 {
		t.Fatalf("reranking metadata mismatch: spec=%s id=%s", m.SpecificationVersion(), m.ModelID())
	}
	if _, err := m.DoRerank(context.Background(), nil); err == nil {
		t.Fatal("DoRerank(nil) expected error")
	}
}
