package huggingface

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// mapHuggingFaceResponsesFinishReason mirrors TS
// mapHuggingFaceResponsesFinishReason.
func mapHuggingFaceResponsesFinishReason(reason string) types.FinishReason {
	switch reason {
	case "stop":
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	case "tool_calls":
		return types.FinishReasonToolCalls
	case "error":
		return types.FinishReasonError
	default:
		return types.FinishReasonOther
	}
}
