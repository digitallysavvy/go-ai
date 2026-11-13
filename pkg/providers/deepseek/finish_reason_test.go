package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapDeepSeekFinishReason(t *testing.T) {
	cases := map[string]types.FinishReason{
		"stop": types.FinishReasonStop, "length": types.FinishReasonLength,
		"content_filter": types.FinishReasonContentFilter, "tool_calls": types.FinishReasonToolCalls,
		"insufficient_system_resource": types.FinishReasonError,
		"function_call":                types.FinishReasonOther, "": types.FinishReasonOther,
	}
	for in, want := range cases {
		if got := mapDeepSeekFinishReason(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}
