package codemode

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TypeScript's code-mode/src/approval-continuation.test.ts ("code-mode
// approval continuations"). "supports callback approval without
// interrupting" is already covered by run_code_mode_test.go's
// ApprovalModeCallback tests (this file is specifically about
// ApprovalModeInterrupt continuations). "exposes run approval batches one
// at a time" and "supports generic host interruptions" are ported in
// continuation_test.go as TestContinueCodeModeApproval_ChainsTwoSequentialApprovals
// and TestContinueCodeModeInterrupt_GenericHostInterruption respectively
// (see the package doc's "Host tool bridge dispatch" section for why the
// former is a chain of two single-item continuations here, not one
// two-item batch).

// "replays completed calls without repeating their side effects".
func TestContinueCodeModeApproval_ReplaysCompletedCallsWithoutRepeatingSideEffects(t *testing.T) {
	lookupCalls, sensitiveCalls := 0, 0
	tools := ToolSet{
		"lookup": {
			Name: "lookup",
			Parameters: map[string]interface{}{
				"type": "object", "required": []interface{}{"id"},
				"properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				lookupCalls++
				return map[string]interface{}{"id": input["id"]}, nil
			},
		},
		"sensitive": {
			Name: "sensitive",
			Parameters: map[string]interface{}{
				"type": "object", "required": []interface{}{"id"},
				"properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
			},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				sensitiveCalls++
				return map[string]interface{}{"id": input["id"], "ok": true}, nil
			},
		},
	}
	js := `
		const first = await tools.lookup({ id: 'item-1' });
		const second = await tools.sensitive({ id: first.id });
		return { first, second };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}

	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:                   js,
		Tools:                tools,
		ToolExecutionOptions: &types.ToolExecutionOptions{ToolCallID: "outer"},
		Options:              options,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pending) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	interrupt := pending.(*Interrupt)
	if interrupt.ToolName != "sensitive" {
		t.Fatalf("got toolName %q, want %q", interrupt.ToolName, "sensitive")
	}
	if interrupt.InterruptID != "outer:tool-2:interrupt" {
		t.Fatalf("got interruptId %q, want %q", interrupt.InterruptID, "outer:tool-2:interrupt")
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup should have been called exactly once, got %d", lookupCalls)
	}
	if sensitiveCalls != 0 {
		t.Fatalf("sensitive should not have been called yet, got %d", sensitiveCalls)
	}

	got, err := ContinueCodeModeApproval(context.Background(), *interrupt, ApprovalResponse{ApprovalID: interrupt.InterruptID, Approved: true}, tools, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{
		"first":  map[string]interface{}{"id": "item-1"},
		"second": map[string]interface{}{"id": "item-1", "ok": true},
	})
	if lookupCalls != 1 {
		t.Fatalf("lookup should still have been called exactly once, got %d", lookupCalls)
	}
	if sensitiveCalls != 1 {
		t.Fatalf("sensitive should have been called exactly once, got %d", sensitiveCalls)
	}
}

// "denies an approval without executing the pending tool".
func TestContinueCodeModeApproval_DeniesWithoutExecutingPendingTool(t *testing.T) {
	executed := false
	tools := ToolSet{"sensitive": {
		Name:          "sensitive",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "should not run", nil
		},
	}}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.sensitive({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pending) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	interrupt := pending.(*Interrupt)

	_, err = ContinueCodeModeApproval(context.Background(), *interrupt, ApprovalResponse{
		ApprovalID: interrupt.InterruptID,
		Approved:   false,
		Reason:     "not allowed",
	}, tools, nil, nil)
	var denied *ToolApprovalDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *ToolApprovalDeniedError, got %#v (%v)", err, err)
	}
	if executed {
		t.Fatal("the pending tool must not execute after a denial")
	}
}

// "rejects interrupt envelopes that do not match the signed ledger".
func TestIsCodeModeApprovalInterrupt_RejectsForgedInterrupt(t *testing.T) {
	tools := ToolSet{"sensitive": {
		Name:          "sensitive",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.sensitive({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pending) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}

	forged := *pending.(*Interrupt)
	forged.ToolName = "different"
	if IsCodeModeApprovalInterrupt(&forged) {
		t.Fatal("expected a forged interrupt (tampered toolName) to be rejected")
	}
}
