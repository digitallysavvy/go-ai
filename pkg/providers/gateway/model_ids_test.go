package gateway

import "testing"

func TestGatewayModelIDConstantsMatchRefreshedSettings(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"language latest OpenAI", string(GatewayLanguageModelOpenaiGpt55), "openai/gpt-5.5"},
		{"language OpenAI 5.2", string(GatewayLanguageModelOpenaiGpt52), "openai/gpt-5.2"},
		{"language latest Anthropic", string(GatewayLanguageModelAnthropicClaudeOpus48), "anthropic/claude-opus-4.8"},
		{"language latest xAI", string(GatewayLanguageModelXaiGrok43), "xai/grok-4.3"},
		{"language Alibaba qwen 3.7", string(GatewayLanguageModelAlibabaQwen37Max), "alibaba/qwen3.7-max"},
		{"language Moonshot code", string(GatewayLanguageModelMoonshotaiKimiK27Code), "moonshotai/kimi-k2.7-code"},
		{"language Mistral medium 3.5", string(GatewayLanguageModelMistralMistralMedium35), "mistral/mistral-medium-3.5"},
		{"language xAI grok build", string(GatewayLanguageModelXaiGrokBuild01), "xai/grok-build-0.1"},
		{"language Zai 5.2", string(GatewayLanguageModelZaiGlm52), "zai/glm-5.2"},
		{"embedding latest Google", string(GatewayEmbeddingModelGoogleGeminiEmbedding2), "google/gemini-embedding-2"},
		{"image latest OpenAI", string(GatewayImageModelOpenaiGptImage2), "openai/gpt-image-2"},
		{"image latest BFL", string(GatewayImageModelBflFlux2Pro), "bfl/flux-2-pro"},
		{"video latest Kling", string(GatewayVideoModelKlingaiKlingV30T2v), "klingai/kling-v3.0-t2v"},
		{"video latest Alibaba", string(GatewayVideoModelAlibabaWanV26T2v), "alibaba/wan-v2.6-t2v"},
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
	if len(GatewayLanguageModelIDs) != 196 {
		t.Fatalf("language catalog length = %d, want refreshed TS catalog", len(GatewayLanguageModelIDs))
	}
	if len(GatewayEmbeddingModelIDs) != 24 {
		t.Fatalf("embedding catalog length = %d, want refreshed TS catalog", len(GatewayEmbeddingModelIDs))
	}
	if len(GatewayImageModelIDs) != 30 {
		t.Fatalf("image catalog length = %d, want refreshed TS catalog", len(GatewayImageModelIDs))
	}
	if len(GatewayVideoModelIDs) != 27 {
		t.Fatalf("video catalog length = %d, want refreshed TS catalog", len(GatewayVideoModelIDs))
	}
	if len(GatewayRerankingModelIDs) != 5 {
		t.Fatalf("reranking catalog length = %d, want refreshed TS catalog", len(GatewayRerankingModelIDs))
	}
	if len(GatewaySpeechModelIDs) != 0 {
		t.Fatalf("speech catalog length = %d, want unconstrained TS catalog", len(GatewaySpeechModelIDs))
	}
	if len(GatewayTranscriptionModelIDs) != 0 {
		t.Fatalf("transcription catalog length = %d, want unconstrained TS catalog", len(GatewayTranscriptionModelIDs))
	}
}

func TestGatewayModelIDCatalogIncludesJune21Additions(t *testing.T) {
	languageIDs := map[GatewayLanguageModelID]bool{}
	for _, id := range GatewayLanguageModelIDs {
		languageIDs[id] = true
	}
	for _, id := range []GatewayLanguageModelID{
		GatewayLanguageModelOpenaiGpt52,
		GatewayLanguageModelOpenaiGpt52Chat,
		GatewayLanguageModelMoonshotaiKimiK27Code,
		GatewayLanguageModelMoonshotaiKimiK27CodeHighspeed,
		GatewayLanguageModelXiaomiMimoV25Pro,
		GatewayLanguageModelZaiGlm52,
	} {
		if !languageIDs[id] {
			t.Fatalf("language catalog missing %q", id)
		}
	}

	videoIDs := map[GatewayVideoModelID]bool{}
	for _, id := range GatewayVideoModelIDs {
		videoIDs[id] = true
	}
	if !videoIDs[GatewayVideoModelXaiGrokImagineVideo15Preview] {
		t.Fatalf("video catalog missing %q", GatewayVideoModelXaiGrokImagineVideo15Preview)
	}
}
