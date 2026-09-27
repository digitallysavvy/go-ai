package perplexity

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// mapPerplexityFinishReason mirrors TS map-perplexity-finish-reason.ts. The
// Agent API reports completion state via `status` plus, for incomplete runs,
// `incomplete_details.reason` -- there is no OpenAI-style top-level
// `finish_reason` string.
func mapPerplexityFinishReason(status, incompleteReason string, hasFunctionCall bool) types.FinishReason {
	switch incompleteReason {
	case "max_output_tokens":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	}

	switch status {
	case "completed":
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonStop
	case "requires_action":
		return types.FinishReasonToolCalls
	case "failed":
		return types.FinishReasonError
	case "cancelled", "queued", "in_progress":
		fallthrough
	default:
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonOther
	}
}
