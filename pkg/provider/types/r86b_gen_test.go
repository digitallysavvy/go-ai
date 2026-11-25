package types

import (
	"encoding/json"
	"testing"
)

// V061 item 23: `"reason": ""` survives decoding and encoding of the
// execution-denied output and the approval response (TS keeps the key; an
// absent reason stays absent).
func TestR86bgenEmptyReasonJSON(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"type":"execution-denied","reason":""}`, `{"type":"execution-denied","reason":""}`},
		{`{"type":"execution-denied"}`, `{"type":"execution-denied"}`},
		{`{"type":"execution-denied","reason":null}`, `{"type":"execution-denied"}`},
		{`{"type":"execution-denied","reason":"no"}`, `{"type":"execution-denied","reason":"no"}`},
	} {
		var out ToolResultOutput
		if err := json.Unmarshal([]byte(c.in), &out); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(out)
		if err != nil || string(data) != c.want {
			t.Errorf("%s -> %s (%v), want %s", c.in, data, err, c.want)
		}
		if got, want := out.ReasonOr("default"), map[bool]string{true: out.Reason, false: "default"}[c.want != `{"type":"execution-denied"}`]; got != want {
			t.Errorf("%s ReasonOr = %q, want %q", c.in, got, want)
		}
	}
	for _, c := range []struct{ in, want string }{
		{`{"type":"tool-approval-response","approvalId":"a","approved":false,"reason":""}`, `{"type":"tool-approval-response","approvalId":"a","approved":false,"reason":""}`},
		{`{"type":"tool-approval-response","approvalId":"a","approved":false}`, `{"type":"tool-approval-response","approvalId":"a","approved":false}`},
	} {
		var part ToolApprovalResponseContent
		if err := json.Unmarshal([]byte(c.in), &part); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(part)
		if err != nil || string(data) != c.want {
			t.Errorf("%s -> %s (%v), want %s", c.in, data, err, c.want)
		}
	}
}
