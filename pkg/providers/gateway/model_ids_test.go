package gateway

import "testing"

func TestGatewayModelIDConstantsMatchRefreshedSettings(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"language latest OpenAI", string(GatewayLanguageModelOpenaiGpt55), "openai/gpt-5.5"},
		{"language latest Anthropic", string(GatewayLanguageModelAnthropicClaudeOpus48), "anthropic/claude-opus-4.8"},
		{"language latest xAI", string(GatewayLanguageModelXaiGrok43), "xai/grok-4.3"},
		{"language Alibaba qwen 3.7", string(GatewayLanguageModelAlibabaQwen37Max), "alibaba/qwen3.7-max"},
		{"language Mistral medium 3.5", string(GatewayLanguageModelMistralMistralMedium35), "mistral/mistral-medium-3.5"},
		{"language xAI grok build", string(GatewayLanguageModelXaiGrokBuild01), "xai/grok-build-0.1"},
		{"embedding latest Google", string(GatewayEmbeddingModelGoogleGeminiEmbedding2), "google/gemini-embedding-2"},
		{"image latest OpenAI", string(GatewayImageModelOpenaiGptImage2), "openai/gpt-image-2"},
		{"video latest Kling", string(GatewayVideoModelKlingaiKlingV30T2v), "klingai/kling-v3.0-t2v"},
		{"reranking latest Cohere", string(GatewayRerankingModelCohereRerankV4Pro), "cohere/rerank-v4-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("constant = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestGatewayModelIDCatalogsExposeAllModelKinds(t *testing.T) {
	if len(GatewayLanguageModelIDs) != 198 {
		t.Fatalf("language catalog length = %d, want refreshed TS catalog", len(GatewayLanguageModelIDs))
	}
	if len(GatewayEmbeddingModelIDs) != 24 {
		t.Fatalf("embedding catalog length = %d, want refreshed TS catalog", len(GatewayEmbeddingModelIDs))
	}
	if len(GatewayImageModelIDs) != 27 {
		t.Fatalf("image catalog length = %d, want refreshed TS catalog", len(GatewayImageModelIDs))
	}
	if len(GatewayVideoModelIDs) != 25 {
		t.Fatalf("video catalog length = %d, want refreshed TS catalog", len(GatewayVideoModelIDs))
	}
	if len(GatewayRerankingModelIDs) != 5 {
		t.Fatalf("reranking catalog length = %d, want refreshed TS catalog", len(GatewayRerankingModelIDs))
	}
}
