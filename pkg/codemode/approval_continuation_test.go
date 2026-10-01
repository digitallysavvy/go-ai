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
// at a time" is ported below as
// TestContinueCodeModeApproval_PromiseAllBatchesBothCallsTogether (see the
// package doc's "Host tool bridge dispatch" section for the concurrent
// approval batching this exercises); "supports generic host
// interruptions" is ported in continuation_test.go as
// TestContinueCodeModeInterrupt_GenericHostInterruption.

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

// Literal port of "exposes run approval batches one at a time", using the
// identical `Promise.all([tools.first({}), tools.second({})])` source
// TypeScript's version uses. Since CM3 (concurrent approval batching, see
// the package doc's "Host tool bridge dispatch" section and
// driveCodeModeExecution in run_code_mode.go), this port's `tools.x(input)`
// dispatch is a genuine async host function: `tools.first({})` and
// `tools.second({})` are both called synchronously while building the
// Promise.all array (neither one's Promise is resolved/rejected/thrown
// yet), so both land in the same pending-interruption batch once the
// sandbox's job queue goes quiescent -- matching TypeScript's "run
// requires the complete interruption batch to be resolved together"
// behavior exactly: neither tool has run at all until *both* approvals are
// given.
func TestContinueCodeModeApproval_PromiseAllBatchesBothCallsTogether(t *testing.T) {
	firstCalls, secondCalls := 0, 0
	tools := ToolSet{
		"first": {
			Name:          "first",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				firstCalls++
				return "first", nil
			},
		},
		"second": {
			Name:          "second",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				secondCalls++
				return "second", nil
			},
		},
	}
	js := `
		const [first, second] = await Promise.all([
			tools.first({}),
			tools.second({}),
		]);
		return { first, second };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}
	toolExecOpts := &types.ToolExecutionOptions{ToolCallID: "outer"}

	pendingFirst, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options, ToolExecutionOptions: toolExecOpts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pendingFirst) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pendingFirst)
	}
	firstInterrupt := pendingFirst.(*Interrupt)
	if firstInterrupt.ToolName != "first" {
		t.Fatalf("expected 'first' to be the batch's first entry, got %#v", firstInterrupt)
	}
	if got := len(firstInterrupt.Continuation.PendingInterruptions); got != 2 {
		t.Fatalf("expected a two-item pending-interruption batch, got %d", got)
	}
	if second := firstInterrupt.Continuation.PendingInterruptions[1]; second.ToolName != "second" {
		t.Fatalf("expected 'second' as the batch's other entry, got %#v", second)
	}
	if firstCalls != 0 || secondCalls != 0 {
		t.Fatalf("neither tool should have executed before any approval: first=%d second=%d", firstCalls, secondCalls)
	}

	pendingSecond, err := ContinueCodeModeApproval(context.Background(), *firstInterrupt, ApprovalResponse{ApprovalID: firstInterrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pendingSecond) {
		t.Fatalf("expected a second pending approval interrupt, got %#v", pendingSecond)
	}
	// Resolving the first entry of an already-collected batch advances
	// straight to the next entry of the *same* batch/continuation -- it
	// does not re-enter the sandbox, so neither tool executes yet.
	if firstCalls != 0 || secondCalls != 0 {
		t.Fatalf("neither tool should have executed after only one approval: first=%d second=%d", firstCalls, secondCalls)
	}

	secondInterrupt := pendingSecond.(*Interrupt)
	final, err := ContinueCodeModeApproval(context.Background(), *secondInterrupt, ApprovalResponse{ApprovalID: secondInterrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, final, map[string]interface{}{"first": "first", "second": "second"})
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("each tool should have executed exactly once: first=%d second=%d", firstCalls, secondCalls)
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
