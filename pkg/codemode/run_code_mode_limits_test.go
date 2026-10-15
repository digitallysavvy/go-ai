package codemode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS `input.toolExecutionOptions?.toolCallId ?? ...` keeps an explicit empty
// toolCallId, so nested ids are ":tool-N".
func TestRunCodeModeHonorsEmptyToolCallID(t *testing.T) {
	tools := ToolSet{
		"sensitive": {
			Name:          "sensitive",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		},
	}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:                   `return await tools.sensitive({});`,
		Tools:                tools,
		ToolExecutionOptions: &types.ToolExecutionOptions{ToolCallID: ""},
		Options:              &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !IsCodeModeApprovalInterrupt(pending) {
		t.Fatalf("expected an approval interrupt, got %#v", pending)
	}
	interrupt := pending.(*Interrupt)
	if interrupt.OuterToolCallID != "" || interrupt.InterruptID != ":tool-1:interrupt" {
		t.Fatalf("outer id %q, interrupt id %q", interrupt.OuterToolCallID, interrupt.InterruptID)
	}
	got, err := ContinueCodeModeApproval(context.Background(), *interrupt, ApprovalResponse{ApprovalID: interrupt.InterruptID, Approved: true}, tools, nil, nil)
	if err != nil || got != "ok" {
		t.Fatalf("resume = %v, %v", got, err)
	}
}

func TestExecutionPolicyRejectsLimitsAboveRunMaximum(t *testing.T) {
	for _, p := range []ExecutionPolicy{{TimeoutMs: 2_147_483_648}, {MaxResultBytes: 1 << 40}} {
		_, err := RunCodeMode(context.Background(), RunInput{JS: "return 1;", Options: &Options{ExecutionPolicy: &p}})
		if err == nil || !strings.Contains(err.Error(), "must be a positive integer") {
			t.Fatalf("policy %+v: err = %v", p, err)
		}
	}
	if _, err := resolveExecutionPolicy(&ExecutionPolicy{TimeoutMs: 2_147_483_647}); err != nil {
		t.Fatal(err)
	}
}

// The continuation signature covers JSON.stringify text: U+2028 stays raw.
func TestCanonicalJSONWritesLineSeparatorsRaw(t *testing.T) {
	ls := string(rune(0x2028))
	got, err := canonicalJSON(map[string]interface{}{"b": "<" + ls, "a": 1})
	if err != nil || got != `{"a":1,"b":"<`+ls+`"}` {
		t.Fatalf("canonicalJSON = %q, %v", got, err)
	}
}

// TS measures JSON.stringify text: '<', '>' and '&' are one byte each.
func TestToJSONPayloadCountsHTMLCharactersAsOneByte(t *testing.T) {
	got, err := toJSONPayload("<>&", 5, "Tool output")
	if err != nil || got != `"<>&"` {
		t.Fatalf("payload = %q, %v", got, err)
	}
}
