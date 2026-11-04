package codemode

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS exceptions.test.ts "detached tool calls" (3fab50aa9b): the detached
// request error names the tool as `tools<property access>`, and a tool name
// with `$&` is inserted literally. Go detects detached calls for interrupted
// (approval-needing) calls, so the cases run in interrupt mode.
func TestR87integDetachedToolCallsNameTheTool(t *testing.T) {
	obj := map[string]interface{}{"type": "object"}
	never := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "should not run", nil
	}
	tools := ToolSet{
		"createSlide":  {Name: "createSlide", Parameters: obj, NeedsApproval: true, Execute: never},
		"update-slide": {Name: "update-slide", Parameters: obj, NeedsApproval: true, Execute: never},
		"slide$&":      {Name: "slide$&", Parameters: obj, NeedsApproval: true, Execute: never},
	}
	for _, tc := range []struct{ js, want string }{
		{"tools.createSlide({}); return 1;", "JavaScript runtime created an unawaited hostFunction bridge request: tools.createSlide."},
		{"tools['update-slide']({}); return 1;", `JavaScript runtime created an unawaited hostFunction bridge request: tools["update-slide"].`},
		{"tools['slide$&']({}); return 1;", `JavaScript runtime created an unawaited hostFunction bridge request: tools["slide$&"].`},
	} {
		_, err := RunCodeMode(context.Background(), RunInput{JS: tc.js, Tools: tools,
			Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}})
		var detached *DetachedBridgeRequestError
		if !errors.As(err, &detached) || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.js, err, tc.want)
		}
	}
}
