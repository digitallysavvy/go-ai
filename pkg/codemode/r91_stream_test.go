package codemode

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91streamActTool(calls *atomic.Int32, approval interface{}) types.Tool {
	return types.Tool{
		Name:         "act",
		Parameters:   map[string]interface{}{"type": "object"},
		ToolApproval: approval,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			calls.Add(1)
			return "done", nil
		},
	}
}

// core3-01 (TS tool-invocation.ts:180 requiresApproval honors every
// needsApproval function): an unnamed approval func literal, a
// user-approval status, a denied status and an unknown value must never run
// the tool unapproved.
func TestR91streamCodeModeApprovalFailsClosed(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		approval interface{}
		check    func(error) bool
	}{
		"func literal with options": {
			approval: func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool { return true },
			check:    func(err error) bool { var e *ToolApprovalRequiredError; return errors.As(err, &e) },
		},
		"legacy func literal": {
			approval: func(context.Context, map[string]interface{}) bool { return true },
			check:    func(err error) bool { var e *ToolApprovalRequiredError; return errors.As(err, &e) },
		},
		"user-approval status": {
			approval: types.ToolApprovalStatusUserApproval,
			check:    func(err error) bool { var e *ToolApprovalRequiredError; return errors.As(err, &e) },
		},
		"user-approval string": {
			approval: "user-approval",
			check:    func(err error) bool { var e *ToolApprovalRequiredError; return errors.As(err, &e) },
		},
		"denied status": {
			approval: types.ToolApprovalStatusDenied,
			check:    func(err error) bool { var e *ToolApprovalDeniedError; return errors.As(err, &e) },
		},
		"unknown status": {
			approval: "maybe",
			check:    func(err error) bool { return err != nil && strings.Contains(err.Error(), "unknown approval status") },
		},
		"unsupported type": {
			approval: 42,
			check: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "unsupported approval setting")
			},
		},
	} {
		var calls atomic.Int32
		_, err := RunCodeMode(ctx, RunInput{JS: "return await tools.act({});", Tools: ToolSet{"act": r91streamActTool(&calls, tc.approval)}})
		if !tc.check(err) {
			t.Errorf("%s: err = %v (%T)", name, err, err)
		}
		if calls.Load() != 0 {
			t.Errorf("%s: tool ran %d times without approval", name, calls.Load())
		}
	}

	// A func literal returning false and the approved / not-applicable
	// statuses run the tool.
	for name, approval := range map[string]interface{}{
		"func false":     func(context.Context, map[string]interface{}) bool { return false },
		"approved":       types.ToolApprovalStatusApproved,
		"not-applicable": "not-applicable",
		"empty":          "",
	} {
		var calls atomic.Int32
		got, err := RunCodeMode(ctx, RunInput{JS: "return await tools.act({});", Tools: ToolSet{"act": r91streamActTool(&calls, approval)}})
		if err != nil || got != "done" || calls.Load() != 1 {
			t.Errorf("%s: got %#v, %v (calls %d)", name, got, err, calls.Load())
		}
	}

	// An approved callback decision runs a func-literal-gated tool.
	var calls atomic.Int32
	got, err := RunCodeMode(ctx, RunInput{
		JS:    "return await tools.act({});",
		Tools: ToolSet{"act": r91streamActTool(&calls, func(context.Context, map[string]interface{}) bool { return true })},
		Options: &Options{Approval: &ApprovalOptions{OnApprovalRequired: func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{Approved: true}, nil
		}}},
	})
	if err != nil || got != "done" || calls.Load() != 1 {
		t.Fatalf("approved: got %#v, %v (calls %d)", got, err, calls.Load())
	}
}

// core3-02 (TS tool-invocation.ts:206 validateToolInput uses
// asSchema(inputSchema).validate): Tool.ValidateInput rejects input before
// approval and execute, and its returned value is the input execute sees.
func TestR91streamCodeModeValidateInput(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	var seen map[string]interface{}
	tool := types.Tool{
		Name:       "act",
		Parameters: map[string]interface{}{"type": "object", "required": []interface{}{"never"}},
		ValidateInput: func(in interface{}) (interface{}, error) {
			m, _ := in.(map[string]interface{})
			if m["bad"] == true {
				return nil, errors.New("bad input")
			}
			return map[string]interface{}{"x": "transformed"}, nil
		},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			calls.Add(1)
			seen = in
			return "done", nil
		},
	}
	_, err := RunCodeMode(ctx, RunInput{JS: "return await tools.act({bad: true});", Tools: ToolSet{"act": tool}})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Error() != `Invalid input for tool "act": bad input` {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("tool ran with rejected input")
	}
	// The validator replaces the schema (whose "required" would fail) and
	// its value reaches Execute.
	got, err := RunCodeMode(ctx, RunInput{JS: "return await tools.act({x: 1});", Tools: ToolSet{"act": tool}})
	if err != nil || got != "done" || seen["x"] != "transformed" {
		t.Fatalf("got %#v, %v, input %v", got, err, seen)
	}
	// A non-object argument the validator accepts is still an input error.
	_, err = RunCodeMode(ctx, RunInput{JS: `return await tools.act("text");`, Tools: ToolSet{"act": {
		Name:          "act",
		ValidateInput: func(in interface{}) (interface{}, error) { return in, nil },
		Execute:       tool.Execute,
	}}})
	if err == nil || !strings.Contains(err.Error(), `Invalid input for tool "act": expected an object, received string`) {
		t.Fatalf("non-object err = %v", err)
	}
}
