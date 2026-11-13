package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapMistralFinishReasonMatchesTS(t *testing.T) {
	cases := map[string]types.FinishReason{
		"stop": types.FinishReasonStop, "length": types.FinishReasonLength, "model_length": types.FinishReasonLength,
		"tool_calls": types.FinishReasonToolCalls, "error": types.FinishReasonError,
		"content_filter": types.FinishReasonOther, "function_call": types.FinishReasonOther,
	}
	for in, want := range cases {
		if got := mapMistralFinishReason(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}
