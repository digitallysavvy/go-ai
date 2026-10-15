package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapBedrockFinishReasonMatchesTS(t *testing.T) {
	cases := map[string]types.FinishReason{
		"end_turn": types.FinishReasonStop, "stop_sequence": types.FinishReasonStop,
		"max_tokens": types.FinishReasonLength, "tool_use": types.FinishReasonToolCalls,
		"content_filtered": types.FinishReasonContentFilter, "guardrail_intervened": types.FinishReasonContentFilter,
		// in BEDROCK_STOP_REASONS but not handled by the TS switch -> other
		"stop": types.FinishReasonOther, "length": types.FinishReasonOther,
		"content-filter": types.FinishReasonOther, "tool-calls": types.FinishReasonOther,
		"model_context_window_exceeded": types.FinishReasonOther, "": types.FinishReasonOther,
	}
	for in, want := range cases {
		if got := mapBedrockFinishReason(in, false); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := mapBedrockFinishReason("tool_use", true); got != types.FinishReasonStop {
		t.Errorf("json tool tool_use -> %q", got)
	}
}
