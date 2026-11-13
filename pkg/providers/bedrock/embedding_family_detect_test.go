package bedrock

import "testing"

// TS detectEmbeddingModelFamily uses String.includes, so cross-region
// inference profile ids keep their family.
func TestDetectEmbeddingModelFamilyUsesIncludes(t *testing.T) {
	cases := map[string]EmbeddingModelFamily{
		"amazon.nova-2-multimodal-embeddings-v1:0":    EmbeddingModelFamilyNova,
		"us.amazon.nova-2-multimodal-embeddings-v1:0": EmbeddingModelFamilyNova,
		"global.cohere.embed-v4:0":                    EmbeddingModelFamilyCohere,
		"apac.eu.cohere.embed-v4:0":                   EmbeddingModelFamilyCohere,
		"amazon.titan-embed-text-v2:0":                EmbeddingModelFamilyTitan,
	}
	for id, want := range cases {
		if got := detectBedrockEmbeddingModelFamily(id); got != want {
			t.Errorf("%s: got %q want %q", id, got, want)
		}
	}
}
