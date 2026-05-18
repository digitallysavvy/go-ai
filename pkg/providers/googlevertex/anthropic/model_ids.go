package anthropic

// GoogleVertexAnthropicModelID is a typed Vertex Anthropic model identifier.
// Raw strings remain accepted by provider methods for the TypeScript SDK's
// string escape-hatch behavior.
type GoogleVertexAnthropicModelID string

const (
	ClaudeOpus4_7              GoogleVertexAnthropicModelID = "claude-opus-4-7"
	ClaudeOpus4_6              GoogleVertexAnthropicModelID = "claude-opus-4-6"
	ClaudeSonnet4_6            GoogleVertexAnthropicModelID = "claude-sonnet-4-6"
	ClaudeOpus4_5_20251101     GoogleVertexAnthropicModelID = "claude-opus-4-5@20251101"
	ClaudeSonnet4_5_20250929   GoogleVertexAnthropicModelID = "claude-sonnet-4-5@20250929"
	ClaudeOpus4_1_20250805     GoogleVertexAnthropicModelID = "claude-opus-4-1@20250805"
	ClaudeOpus4_20250514       GoogleVertexAnthropicModelID = "claude-opus-4@20250514"
	ClaudeSonnet4_20250514     GoogleVertexAnthropicModelID = "claude-sonnet-4@20250514"
	Claude3_7Sonnet_20250219   GoogleVertexAnthropicModelID = "claude-3-7-sonnet@20250219"
	Claude3_5SonnetV2_20241022 GoogleVertexAnthropicModelID = "claude-3-5-sonnet-v2@20241022"
	Claude3_5Haiku_20241022    GoogleVertexAnthropicModelID = "claude-3-5-haiku@20241022"
	Claude3_5Sonnet_20240620   GoogleVertexAnthropicModelID = "claude-3-5-sonnet@20240620"
	Claude3Haiku_20240307      GoogleVertexAnthropicModelID = "claude-3-haiku@20240307"
	Claude3Sonnet_20240229     GoogleVertexAnthropicModelID = "claude-3-sonnet@20240229"
	Claude3Opus_20240229       GoogleVertexAnthropicModelID = "claude-3-opus@20240229"
)
