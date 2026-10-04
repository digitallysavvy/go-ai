package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// Mirrors TS map-xai-responses-finish-reason.ts plus the response.incomplete
// branch (reason ? map(reason) : 'other').
func TestMapXAIResponsesFinishReason(t *testing.T) {
	reason := func(r string) *responses.IncompleteDetails { return &responses.IncompleteDetails{Reason: r} }
	cases := []struct {
		status  string
		details *responses.IncompleteDetails
		want    types.FinishReason
	}{
		{"completed", nil, types.FinishReasonStop},
		{"stop", nil, types.FinishReasonStop},
		{"length", nil, types.FinishReasonLength},
		{"max_output_tokens", nil, types.FinishReasonLength},
		{"tool_calls", nil, types.FinishReasonToolCalls},
		{"function_call", nil, types.FinishReasonToolCalls},
		{"content_filter", nil, types.FinishReasonContentFilter},
		{"", nil, types.FinishReasonOther},
		{"in_progress", nil, types.FinishReasonOther},
		{"incomplete", reason("max_output_tokens"), types.FinishReasonLength},
		{"incomplete", reason("content_filter"), types.FinishReasonContentFilter},
		{"incomplete", nil, types.FinishReasonOther},
	}
	for _, c := range cases {
		if got := mapXAIResponsesFinishReason(c.status, c.details); got != c.want {
			t.Errorf("mapXAIResponsesFinishReason(%q, %+v) = %q, want %q", c.status, c.details, got, c.want)
		}
	}
}
