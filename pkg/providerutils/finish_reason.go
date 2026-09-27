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
	default:
		return types.FinishReasonOther
	}
}
