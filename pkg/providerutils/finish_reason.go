package providerutils

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MapOpenAIFinishReason maps OpenAI-compatible finish reason strings to SDK types.
// Handles both current ("tool_calls") and legacy ("function_call") values.
func MapOpenAIFinishReason(reason string) types.FinishReason {
	switch reason {
	case "stop":
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonLength
	case "tool_calls", "function_call":
		return types.FinishReasonToolCalls
	case "content_filter":
		return types.FinishReasonContentFilter
	// Z.AI-specific finish reasons (mirrors TS mapZaiFinishReason in
	// zai-chat-language-model.ts). These raw strings are not emitted by any
	// other OpenAI-compatible provider in this SDK, so adding them here is
	// safe for all other callers of this shared mapper.
	case "sensitive":
		return types.FinishReasonContentFilter
	case "model_context_window_exceeded":
		return types.FinishReasonLength
	case "network_error":
		return types.FinishReasonError
	default:
		return types.FinishReasonOther
	}
}
