package openresponses

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MapOpenResponsesFinishReason maps Open Responses finish reasons to AI SDK finish reasons
func MapOpenResponsesFinishReason(reason string, hasToolCalls bool) types.FinishReason {
	// If there are tool calls, it's a tool-calls finish
	if hasToolCalls {
		return types.FinishReasonToolCalls
	}

	// Map based on the incomplete reason
	switch reason {
	case "":
		// Empty reason typically means completed successfully
		return types.FinishReasonStop

	case "max_output_tokens", "max_tokens":
		return types.FinishReasonLength

	case "content_filter":
		return types.FinishReasonContentFilter

	case "tool_calls":
		return types.FinishReasonToolCalls

	case "stop":
		return types.FinishReasonStop

	case "error":
		return types.FinishReasonError

	default:
		return types.FinishReasonOther
	}
}
