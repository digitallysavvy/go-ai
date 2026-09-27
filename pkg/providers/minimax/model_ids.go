package minimax

// Language model ID constants for MiniMax chat models
// (https://platform.minimax.io/docs/api-reference/text-chat-anthropic).
// Mirrors packages/minimax/src/minimax-chat-options.ts (MiniMaxChatModelId)
// at ai@7.0.113. Any model ID string is accepted; these constants exist for
// convenience and IDE support.
const (
	ModelM3           = "minimax-m3"
	ModelM27          = "minimax-m2.7"
	ModelM27HighSpeed = "minimax-m2.7-highspeed"
	ModelM25          = "minimax-m2.5"
	ModelM25HighSpeed = "minimax-m2.5-highspeed"
	ModelM21          = "minimax-m2.1"
	ModelM21HighSpeed = "minimax-m2.1-highspeed"
	ModelM2           = "minimax-m2"
)
