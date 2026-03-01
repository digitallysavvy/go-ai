package bedrock

import "testing"

// TestBedrockAnthropicModelIDs verifies that the Anthropic model ID constants
// have the correct string values as defined by the Bedrock API.
func TestBedrockAnthropicModelIDs(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		// Non-cross-region Anthropic models
		{"ClaudeOpus4_6V1", ModelAnthropicClaudeOpus4_6V1, "anthropic.claude-opus-4-6-v1"},
		{"ClaudeSonnet4_6V1", ModelAnthropicClaudeSonnet4_6V1, "anthropic.claude-sonnet-4-6-v1"},
		{"ClaudeOpus45_V1", ModelAnthropicClaudeOpus45_V1, "anthropic.claude-opus-4-5-20251101-v1:0"},
		{"ClaudeHaiku45_V1", ModelAnthropicClaudeHaiku45_V1, "anthropic.claude-haiku-4-5-20251001-v1:0"},
		{"ClaudeSonnet45_V1", ModelAnthropicClaudeSonnet45_V1, "anthropic.claude-sonnet-4-5-20250929-v1:0"},
		{"ClaudeSonnet4_V1", ModelAnthropicClaudeSonnet4_V1, "anthropic.claude-sonnet-4-20250514-v1:0"},
		{"ClaudeOpus4_V1", ModelAnthropicClaudeOpus4_V1, "anthropic.claude-opus-4-20250514-v1:0"},
		{"ClaudeOpus41_V1", ModelAnthropicClaudeOpus41_V1, "anthropic.claude-opus-4-1-20250805-v1:0"},
		{"Claude37Sonnet_V1", ModelAnthropicClaude37Sonnet_V1, "anthropic.claude-3-7-sonnet-20250219-v1:0"},
		{"Claude35Sonnet_V1", ModelAnthropicClaude35Sonnet_V1, "anthropic.claude-3-5-sonnet-20240620-v1:0"},
		{"Claude35SonnetV2_V1", ModelAnthropicClaude35SonnetV2_V1, "anthropic.claude-3-5-sonnet-20241022-v2:0"},
		{"Claude35Haiku_V1", ModelAnthropicClaude35Haiku_V1, "anthropic.claude-3-5-haiku-20241022-v1:0"},
		{"Claude3Sonnet_V1", ModelAnthropicClaude3Sonnet_V1, "anthropic.claude-3-sonnet-20240229-v1:0"},
		{"Claude3Haiku_V1", ModelAnthropicClaude3Haiku_V1, "anthropic.claude-3-haiku-20240307-v1:0"},
		{"Claude3Opus_V1", ModelAnthropicClaude3Opus_V1, "anthropic.claude-3-opus-20240229-v1:0"},

		// US cross-region Anthropic models (latest/new ones)
		{"USClaudeOpus4_6V1", ModelUSAnthropicClaudeOpus4_6V1, "us.anthropic.claude-opus-4-6-v1"},
		{"USClaudeSonnet4_6V1", ModelUSAnthropicClaudeSonnet4_6V1, "us.anthropic.claude-sonnet-4-6-v1"},
		{"USClaudeOpus45_V1", ModelUSAnthropicClaudeOpus45_V1, "us.anthropic.claude-opus-4-5-20251101-v1:0"},
		{"USClaudeHaiku45_V1", ModelUSAnthropicClaudeHaiku45_V1, "us.anthropic.claude-haiku-4-5-20251001-v1:0"},
		{"USClaudeSonnet45_V1", ModelUSAnthropicClaudeSonnet45_V1, "us.anthropic.claude-sonnet-4-5-20250929-v1:0"},
		{"USClaudeSonnet4_V1", ModelUSAnthropicClaudeSonnet4_V1, "us.anthropic.claude-sonnet-4-20250514-v1:0"},
		{"USClaudeOpus4_V1", ModelUSAnthropicClaudeOpus4_V1, "us.anthropic.claude-opus-4-20250514-v1:0"},
		{"USClaudeOpus41_V1", ModelUSAnthropicClaudeOpus41_V1, "us.anthropic.claude-opus-4-1-20250805-v1:0"},
		{"USClaude37Sonnet_V1", ModelUSAnthropicClaude37Sonnet_V1, "us.anthropic.claude-3-7-sonnet-20250219-v1:0"},
		{"USClaude35Sonnet_V1", ModelUSAnthropicClaude35Sonnet_V1, "us.anthropic.claude-3-5-sonnet-20240620-v1:0"},
		{"USClaude35Haiku_V1", ModelUSAnthropicClaude35Haiku_V1, "us.anthropic.claude-3-5-haiku-20241022-v1:0"},
		{"USClaude35SonnetV2_V1", ModelUSAnthropicClaude35SonnetV2_V1, "us.anthropic.claude-3-5-sonnet-20241022-v2:0"},
		{"USClaude3Sonnet_V1", ModelUSAnthropicClaude3Sonnet_V1, "us.anthropic.claude-3-sonnet-20240229-v1:0"},
		{"USClaude3Opus_V1", ModelUSAnthropicClaude3Opus_V1, "us.anthropic.claude-3-opus-20240229-v1:0"},
		{"USClaude3Haiku_V1", ModelUSAnthropicClaude3Haiku_V1, "us.anthropic.claude-3-haiku-20240307-v1:0"},

		// Amazon Nova
		{"AmazonNovaPremier", ModelAmazonNovaPremierV1, "us.amazon.nova-premier-v1:0"},
		{"AmazonNovaProV1", ModelAmazonNovaProV1, "us.amazon.nova-pro-v1:0"},
		{"AmazonNovaMicroV1", ModelAmazonNovaMicroV1, "us.amazon.nova-micro-v1:0"},
		{"AmazonNovaLiteV1", ModelAmazonNovaLiteV1, "us.amazon.nova-lite-v1:0"},

		// DeepSeek
		{"USDeepSeekR1V1", ModelUSDeepSeekR1V1, "us.deepseek.r1-v1:0"},

		// Llama 4
		{"USLlama4Scout", ModelUSMetaLlama4Scout17BInstructV1, "us.meta.llama4-scout-17b-instruct-v1:0"},
		{"USLlama4Maverick", ModelUSMetaLlama4Maverick17BInstructV1, "us.meta.llama4-maverick-17b-instruct-v1:0"},

		// Mistral cross-region
		{"USMistralPixtral", ModelUSMistralPixtralLarge2502V1, "us.mistral.pixtral-large-2502-v1:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("model ID constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// TestBedrockModelIDsAcceptedByProvider verifies a sample of model IDs are accepted
// by the provider's LanguageModel() method.
func TestBedrockModelIDsAcceptedByProvider(t *testing.T) {
	p := New(Config{
		AWSAccessKeyID:     "test-id",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})

	modelIDs := []string{
		ModelUSAnthropicClaudeOpus4_6V1,
		ModelUSAnthropicClaudeSonnet4_6V1,
		ModelUSAnthropicClaude37Sonnet_V1,
		ModelUSAnthropicClaude35Haiku_V1,
		ModelAmazonNovaProV1,
		ModelUSDeepSeekR1V1,
		ModelUSMetaLlama4Scout17BInstructV1,
	}

	for _, id := range modelIDs {
		t.Run(id, func(t *testing.T) {
			model, err := p.LanguageModel(id)
			if err != nil {
				t.Fatalf("LanguageModel(%q) returned error: %v", id, err)
			}
			if model.ModelID() != id {
				t.Errorf("ModelID() = %q, want %q", model.ModelID(), id)
			}
		})
	}
}
