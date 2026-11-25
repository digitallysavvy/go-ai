package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// V061 item 23 (cr-r86b-gen-to-carry 4): TS gen-ai-format-messages builds
// {denied: true, reason: output.reason} and `reason: part.reason` on approval
// responses; JSON.stringify drops an undefined reason and keeps "".
func TestR86bintegTelemetryDeniedReasonPresence(t *testing.T) {
	marshal := func(v interface{}) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	denied := func(o types.ToolResultOutput) string {
		return marshal(toolResultResponseValue(types.ToolResultContent{ToolCallID: "c", ToolName: "t", Output: &o}))
	}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"denied absent", denied(types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}), `{"denied":true}`},
		{"denied empty given", denied(types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, ReasonSet: true}), `{"denied":true,"reason":""}`},
		{"denied text", denied(types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "no"}), `{"denied":true,"reason":"no"}`},
		{"response absent", marshal(convertMessagePartToSemConv(types.ToolApprovalResponseContent{ApprovalID: "a", Approved: false})), `{"type":"tool_approval_response","approval_id":"a","approved":false}`},
		{"response empty given", marshal(convertMessagePartToSemConv(types.ToolApprovalResponseContent{ApprovalID: "a", ReasonSet: true})), `{"type":"tool_approval_response","approval_id":"a","approved":false,"reason":""}`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, tc.got, tc.want)
		}
	}
}
