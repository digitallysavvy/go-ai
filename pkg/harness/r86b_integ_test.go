package harness

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// V061 item 23 (cr-r86b-gen-to-carry 2): TS
// convert-harness-tool-model-output.ts `output.reason ?? 'Tool execution
// denied.'` keeps a given empty reason.
func TestR86bintegHarnessDeniedReasonKept(t *testing.T) {
	for _, tc := range []struct {
		out  types.ToolResultOutput
		want string
	}{
		{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}, "Tool execution denied."},
		{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, ReasonSet: true}, ""},
		{types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "no"}, "no"},
	} {
		got, isErr, err := ConvertToolModelOutput(tc.out)
		if err != nil || !isErr || len(got) != 1 || got[0].Text != tc.want {
			t.Errorf("ConvertToolModelOutput(%+v) = %+v, %v, %v; want text %q", tc.out, got, isErr, err, tc.want)
		}
	}
}
