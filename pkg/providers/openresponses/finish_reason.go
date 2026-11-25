package openresponses

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MapOpenResponsesFinishReason maps Open Responses finish reasons to AI SDK
// finish reasons (the v0.5 mapping, kept for callers of this helper). The
// language model maps like TS mapOpenResponsesFinishReason instead (see
// mapOpenResponsesFinishReasonTS).
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

// mapOpenResponsesFinishReasonTS is TS mapOpenResponsesFinishReason: with no
// (or a null) incomplete reason, tool calls give tool-calls and otherwise
// stop; max_output_tokens is length and content_filter content-filter even
// with tool calls; any other reason ("" included) is tool-calls with tool
// calls, else other. present reports a non-null reason.
func mapOpenResponsesFinishReasonTS(reason string, present, hasToolCalls bool) types.FinishReason {
	if !present {
		if hasToolCalls {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonStop
	}
	switch reason {
	case "max_output_tokens":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		if hasToolCalls {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonOther
	}
}
