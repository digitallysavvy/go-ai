package bedrock

// Language model ID constants for AWS Bedrock.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://docs.aws.amazon.com/bedrock/latest/userguide/model-ids.html for the full list.
const (
	// ─── Amazon Titan ────────────────────────────────────────────────────────

	ModelAmazonTitanTG1Large       = "amazon.titan-tg1-large"
	ModelAmazonTitanTextExpressV1  = "amazon.titan-text-express-v1"
	ModelAmazonTitanTextLiteV1     = "amazon.titan-text-lite-v1"

	// ─── Amazon Nova ─────────────────────────────────────────────────────────

	ModelAmazonNovaPremierV1 = "us.amazon.nova-premier-v1:0"
	ModelAmazonNovaProV1     = "us.amazon.nova-pro-v1:0"
	ModelAmazonNovaMicroV1   = "us.amazon.nova-micro-v1:0"
	ModelAmazonNovaLiteV1    = "us.amazon.nova-lite-v1:0"

	// ─── Anthropic Claude (non-cross-region) ─────────────────────────────────

	ModelAnthropicClaudeV2            = "anthropic.claude-v2"
	ModelAnthropicClaudeV2_1          = "anthropic.claude-v2:1"
	ModelAnthropicClaudeInstantV1     = "anthropic.claude-instant-v1"
	ModelAnthropicClaudeOpus4_6V1     = "anthropic.claude-opus-4-6-v1"
	ModelAnthropicClaudeSonnet4_6V1   = "anthropic.claude-sonnet-4-6-v1"
	ModelAnthropicClaudeOpus45_V1     = "anthropic.claude-opus-4-5-20251101-v1:0"
	ModelAnthropicClaudeHaiku45_V1    = "anthropic.claude-haiku-4-5-20251001-v1:0"
	ModelAnthropicClaudeSonnet45_V1   = "anthropic.claude-sonnet-4-5-20250929-v1:0"
	ModelAnthropicClaudeSonnet4_V1    = "anthropic.claude-sonnet-4-20250514-v1:0"
	ModelAnthropicClaudeOpus4_V1      = "anthropic.claude-opus-4-20250514-v1:0"
	ModelAnthropicClaudeOpus41_V1     = "anthropic.claude-opus-4-1-20250805-v1:0"
	ModelAnthropicClaude37Sonnet_V1   = "anthropic.claude-3-7-sonnet-20250219-v1:0"
	ModelAnthropicClaude35Sonnet_V1   = "anthropic.claude-3-5-sonnet-20240620-v1:0"
	ModelAnthropicClaude35SonnetV2_V1 = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	ModelAnthropicClaude35Haiku_V1    = "anthropic.claude-3-5-haiku-20241022-v1:0"
	ModelAnthropicClaude3Sonnet_V1    = "anthropic.claude-3-sonnet-20240229-v1:0"
	ModelAnthropicClaude3Haiku_V1     = "anthropic.claude-3-haiku-20240307-v1:0"
	ModelAnthropicClaude3Opus_V1      = "anthropic.claude-3-opus-20240229-v1:0"

	// ─── Anthropic Claude (us cross-region) ──────────────────────────────────

	ModelUSAnthropicClaude3Sonnet_V1    = "us.anthropic.claude-3-sonnet-20240229-v1:0"
	ModelUSAnthropicClaude3Opus_V1      = "us.anthropic.claude-3-opus-20240229-v1:0"
	ModelUSAnthropicClaude3Haiku_V1     = "us.anthropic.claude-3-haiku-20240307-v1:0"
	ModelUSAnthropicClaude35Sonnet_V1   = "us.anthropic.claude-3-5-sonnet-20240620-v1:0"
	ModelUSAnthropicClaude35Haiku_V1    = "us.anthropic.claude-3-5-haiku-20241022-v1:0"
	ModelUSAnthropicClaude35SonnetV2_V1 = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
	ModelUSAnthropicClaude37Sonnet_V1   = "us.anthropic.claude-3-7-sonnet-20250219-v1:0"
	ModelUSAnthropicClaudeOpus4_6V1     = "us.anthropic.claude-opus-4-6-v1"
	ModelUSAnthropicClaudeSonnet4_6V1   = "us.anthropic.claude-sonnet-4-6-v1"
	ModelUSAnthropicClaudeOpus45_V1     = "us.anthropic.claude-opus-4-5-20251101-v1:0"
	ModelUSAnthropicClaudeSonnet45_V1   = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	ModelUSAnthropicClaudeSonnet4_V1    = "us.anthropic.claude-sonnet-4-20250514-v1:0"
	ModelUSAnthropicClaudeOpus4_V1      = "us.anthropic.claude-opus-4-20250514-v1:0"
	ModelUSAnthropicClaudeOpus41_V1     = "us.anthropic.claude-opus-4-1-20250805-v1:0"
	ModelUSAnthropicClaudeHaiku45_V1    = "us.anthropic.claude-haiku-4-5-20251001-v1:0"

	// ─── Cohere ──────────────────────────────────────────────────────────────

	ModelCohereCommandTextV14      = "cohere.command-text-v14"
	ModelCohereCommandLightTextV14 = "cohere.command-light-text-v14"
	ModelCohereCommandRV1          = "cohere.command-r-v1:0"
	ModelCohereCommandRPlusV1      = "cohere.command-r-plus-v1:0"

	// ─── Meta Llama 3 ────────────────────────────────────────────────────────

	ModelMetaLlama370BInstructV1  = "meta.llama3-70b-instruct-v1:0"
	ModelMetaLlama38BInstructV1   = "meta.llama3-8b-instruct-v1:0"
	ModelMetaLlama31405BInstructV1 = "meta.llama3-1-405b-instruct-v1:0"
	ModelMetaLlama3170BInstructV1 = "meta.llama3-1-70b-instruct-v1:0"
	ModelMetaLlama318BInstructV1  = "meta.llama3-1-8b-instruct-v1:0"
	ModelMetaLlama3211BInstructV1 = "meta.llama3-2-11b-instruct-v1:0"
	ModelMetaLlama321BInstructV1  = "meta.llama3-2-1b-instruct-v1:0"
	ModelMetaLlama323BInstructV1  = "meta.llama3-2-3b-instruct-v1:0"
	ModelMetaLlama3290BInstructV1 = "meta.llama3-2-90b-instruct-v1:0"

	// ─── Meta Llama 3 (us cross-region) ──────────────────────────────────────

	ModelUSMetaLlama3211BInstructV1 = "us.meta.llama3-2-11b-instruct-v1:0"
	ModelUSMetaLlama323BInstructV1  = "us.meta.llama3-2-3b-instruct-v1:0"
	ModelUSMetaLlama3290BInstructV1 = "us.meta.llama3-2-90b-instruct-v1:0"
	ModelUSMetaLlama321BInstructV1  = "us.meta.llama3-2-1b-instruct-v1:0"
	ModelUSMetaLlama318BInstructV1  = "us.meta.llama3-1-8b-instruct-v1:0"
	ModelUSMetaLlama3170BInstructV1 = "us.meta.llama3-1-70b-instruct-v1:0"
	ModelUSMetaLlama3370BInstructV1 = "us.meta.llama3-3-70b-instruct-v1:0"

	// ─── Meta Llama 4 (us cross-region) ──────────────────────────────────────

	ModelUSMetaLlama4Scout17BInstructV1    = "us.meta.llama4-scout-17b-instruct-v1:0"
	ModelUSMetaLlama4Maverick17BInstructV1 = "us.meta.llama4-maverick-17b-instruct-v1:0"

	// ─── Mistral ─────────────────────────────────────────────────────────────

	ModelMistral7BInstructV0         = "mistral.mistral-7b-instruct-v0:2"
	ModelMistralMixtral8x7BInstructV0 = "mistral.mixtral-8x7b-instruct-v0:1"
	ModelMistralLarge2402V1          = "mistral.mistral-large-2402-v1:0"
	ModelMistralSmall2402V1          = "mistral.mistral-small-2402-v1:0"
	ModelUSMistralPixtralLarge2502V1  = "us.mistral.pixtral-large-2502-v1:0"

	// ─── OpenAI (via Bedrock) ─────────────────────────────────────────────────

	ModelOpenAIGPTOSS120B = "openai.gpt-oss-120b-1:0"
	ModelOpenAIGPTOSS20B  = "openai.gpt-oss-20b-1:0"

	// ─── DeepSeek (us cross-region) ──────────────────────────────────────────

	ModelUSDeepSeekR1V1 = "us.deepseek.r1-v1:0"
)
