package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Reviewer item 5 (r80): TS getToolPartInputSchemaInput uses hasOwnProperty,
// so `"inputSchemaInput": null` is validated (null against the tool input
// schema) instead of the part input, and it survives serialization.
func TestR80StreamApprovalInputSchemaInputNull(t *testing.T) {
	countTool := types.Tool{
		Name: "count",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"count"},
		},
	}
	_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true,"inputSchemaInput":null}}
		]}]`),
		Tools: []types.Tool{countTool},
	})
	if err == nil {
		t.Fatal("null inputSchemaInput validated the part input instead of null")
	}
	if !strings.Contains(err.Error(), "messages[0].parts[0].input") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Absent key: the part input is validated (and passes).
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true}}
		]}]`),
		Tools: []types.Tool{countTool},
	}); err != nil {
		t.Fatalf("absent inputSchemaInput: %v", err)
	}

	// Round trip keeps the null key; an absent key stays absent.
	for _, tc := range []struct{ in, want string }{
		{`{"id":"a","approved":true,"inputSchemaInput":null}`, `{"id":"a","approved":true,"inputSchemaInput":null}`},
		{`{"id":"a","approved":true}`, `{"id":"a","approved":true}`},
		{`{"id":"a","inputSchemaInput":{"x":1}}`, `{"id":"a","inputSchemaInput":{"x":1}}`},
	} {
		var a ToolUIPartApproval
		if err := json.Unmarshal([]byte(tc.in), &a); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("round trip %s: got %s, want %s", tc.in, got, tc.want)
		}
	}
}
