package codemode

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Additional CM3 coverage beyond
// TestContinueCodeModeApproval_PromiseAllBatchesBothCallsTogether
// (approval_continuation_test.go), which is the literal port of
// TypeScript's "exposes run approval batches one at a time": mixed
// approved/denied decisions within one batch, one call that needs
// approval alongside one that does not in the same Promise.all, a nested
// Promise.all, and resuming with a tampered continuation. See the package
// doc's "Host tool bridge dispatch" section and driveCodeModeExecution's
// doc comment (run_code_mode.go) for the batching design these exercise.

// A batch where one entry is ultimately denied must not execute *either*
// tool: TypeScript's run requires every pending interruption in a batch to
// be resolved before replaying continues (assertContinuationResolutions in
// `run`'s manager.js), and assertNoDeniedApproval (run_code_mode.go)
// mirrors that by checking every resolution -- including ones supplied
// before the denied one -- before any real execution happens.
func TestConcurrentApprovalBatch_MixedApprovedAndDenied(t *testing.T) {
	var mu sync.Mutex
	approveCalls, denyCalls := 0, 0
	tools := ToolSet{
		"approveMe": {
			Name:          "approveMe",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				approveCalls++
				mu.Unlock()
				return "approved-result", nil
			},
		},
		"denyMe": {
			Name:          "denyMe",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				denyCalls++
				mu.Unlock()
				return "should not run", nil
			},
		},
	}
	js := `
		const [a, b] = await Promise.all([
			tools.approveMe({}),
			tools.denyMe({}),
		]);
		return { a, b };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}

	pending, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstInterrupt, ok := pending.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(firstInterrupt) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	if got := len(firstInterrupt.Continuation.PendingInterruptions); got != 2 {
		t.Fatalf("expected a two-item batch, got %d", got)
	}

	// Approve the first entry (approveMe) -- still no execution, since the
	// batch isn't fully resolved yet.
	pendingSecond, err := ContinueCodeModeApproval(context.Background(), *firstInterrupt,
		ApprovalResponse{ApprovalID: firstInterrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error resolving the first entry: %v", err)
	}
	secondInterrupt, ok := pendingSecond.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(secondInterrupt) {
		t.Fatalf("expected a second pending approval interrupt, got %#v", pendingSecond)
	}

	// Deny the second entry (denyMe) -- the whole batch must now fail, and
	// neither tool must have executed, even though "approveMe" was
	// individually approved.
	_, err = ContinueCodeModeApproval(context.Background(), *secondInterrupt,
		ApprovalResponse{ApprovalID: secondInterrupt.InterruptID, Approved: false, Reason: "nope"}, tools, options, nil)
	var denied *ToolApprovalDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *ToolApprovalDeniedError, got %#v (%v)", err, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if approveCalls != 0 {
		t.Fatalf("approveMe must not execute when a sibling in the same batch is denied, got %d calls", approveCalls)
	}
	if denyCalls != 0 {
		t.Fatalf("denyMe must not execute after being denied, got %d calls", denyCalls)
	}
}

// One tool in a Promise.all needs approval; the other does not and
// completes for real, synchronously, within the same wave. Only the
// approval-needing call appears in the pending batch; the other tool's
// real result is already committed (and is not re-invoked on resume).
func TestConcurrentApprovalBatch_OneNeedsApprovalOneDoesNot(t *testing.T) {
	var mu sync.Mutex
	guardedCalls, freeCalls := 0, 0
	tools := ToolSet{
		"guarded": {
			Name:          "guarded",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				guardedCalls++
				mu.Unlock()
				return "guarded-result", nil
			},
		},
		"free": {
			Name:       "free",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				freeCalls++
				mu.Unlock()
				return "free-result", nil
			},
		},
	}
	js := `
		const [g, f] = await Promise.all([
			tools.guarded({}),
			tools.free({}),
		]);
		return { g, f };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}

	pending, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt, ok := pending.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(interrupt) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	if got := len(interrupt.Continuation.PendingInterruptions); got != 1 {
		t.Fatalf("expected a single-item batch (only the approval-needing call), got %d", got)
	}
	if interrupt.ToolName != "guarded" {
		t.Fatalf("expected 'guarded' to be the pending entry, got %q", interrupt.ToolName)
	}

	mu.Lock()
	if freeCalls != 1 {
		t.Fatalf("'free' should already have executed once (no approval needed), got %d", freeCalls)
	}
	if guardedCalls != 0 {
		t.Fatalf("'guarded' should not have executed before approval, got %d", guardedCalls)
	}
	mu.Unlock()

	final, err := ContinueCodeModeApproval(context.Background(), *interrupt,
		ApprovalResponse{ApprovalID: interrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, final, map[string]interface{}{"g": "guarded-result", "f": "free-result"})

	mu.Lock()
	defer mu.Unlock()
	if guardedCalls != 1 {
		t.Fatalf("'guarded' should have executed exactly once, got %d", guardedCalls)
	}
	if freeCalls != 1 {
		t.Fatalf("'free' must not be re-invoked on resume (its result was already committed), got %d calls", freeCalls)
	}
}

// A nested Promise.all still collects every concurrently-dispatched call
// into the same batch: the inner array's elements are evaluated
// synchronously while building it, exactly like the outer array's, all
// before the sandbox's job queue ever goes quiescent.
func TestConcurrentApprovalBatch_NestedPromiseAll(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	makeTool := func(name string) types.Tool {
		return types.Tool{
			Name:          name,
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				calls[name]++
				mu.Unlock()
				return name + "-result", nil
			},
		}
	}
	tools := ToolSet{"a": makeTool("a"), "b": makeTool("b"), "c": makeTool("c")}
	js := `
		const [a, [b, c]] = await Promise.all([
			tools.a({}),
			Promise.all([tools.b({}), tools.c({})]),
		]);
		return { a, b, c };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}

	pending, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt, ok := pending.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(interrupt) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	batch := interrupt.Continuation.PendingInterruptions
	if len(batch) != 3 {
		t.Fatalf("expected all three concurrently-dispatched calls (a, b, c) in one batch, got %d: %#v", len(batch), batch)
	}
	gotNames := []string{batch[0].ToolName, batch[1].ToolName, batch[2].ToolName}
	wantNames := []string{"a", "b", "c"}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("expected dispatch order a, b, c (array-literal evaluation order); got %v", gotNames)
		}
	}

	mu.Lock()
	if len(calls) != 0 {
		t.Fatalf("no tool should have executed before any approval, got %#v", calls)
	}
	mu.Unlock()

	// Approve all three in order.
	current := interrupt
	var result interface{}
	for i := 0; i < 3; i++ {
		result, err = ContinueCodeModeApproval(context.Background(), *current,
			ApprovalResponse{ApprovalID: current.InterruptID, Approved: true}, tools, options, nil)
		if err != nil {
			t.Fatalf("unexpected error approving entry %d: %v", i, err)
		}
		if i < 2 {
			next, ok := result.(*Interrupt)
			if !ok || !IsCodeModeApprovalInterrupt(next) {
				t.Fatalf("expected another pending approval interrupt after entry %d, got %#v", i, result)
			}
			current = next
		}
	}
	assertDeepEqual(t, result, map[string]interface{}{"a": "a-result", "b": "b-result", "c": "c-result"})

	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"a", "b", "c"} {
		if calls[name] != 1 {
			t.Fatalf("tool %q should have executed exactly once, got %d", name, calls[name])
		}
	}
}

// Tampering with a pending-interruption batch's signed Continuation
// payload -- even an entry other than the one currently being resolved,
// which the ledger-consistency check (assertInterruptMatchesLedger) does
// not itself inspect -- must be caught by the HMAC-SHA256 signature
// verification when resuming, exactly as a tampered single-item
// continuation already is (TestVerifyCodeModeContinuation_RejectsTamperedEnvelope).
func TestConcurrentApprovalBatch_ResumeWithTamperedContinuationFailsSignatureVerification(t *testing.T) {
	tools := ToolSet{
		"first": {
			Name:          "first",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return "first-result", nil
			},
		},
		"second": {
			Name:          "second",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return "second-result", nil
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

	pending, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt, ok := pending.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(interrupt) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	if len(interrupt.Continuation.PendingInterruptions) != 2 {
		t.Fatalf("expected a two-item batch, got %d", len(interrupt.Continuation.PendingInterruptions))
	}

	// Sanity check: the untampered continuation resumes cleanly.
	untampered := *interrupt
	if _, verr := ContinueCodeModeApproval(context.Background(), untampered,
		ApprovalResponse{ApprovalID: untampered.InterruptID, Approved: true}, tools, options, nil); verr != nil {
		t.Fatalf("expected the untampered continuation to resume cleanly, got %v", verr)
	}

	// Tamper the *second* batch entry -- not the one being resolved right
	// now (index 0) -- so assertInterruptMatchesLedger's own
	// index-0-only consistency check cannot catch it; only the signature
	// over the whole continuation payload can.
	tampered := *interrupt
	tampered.Continuation.PendingInterruptions = append(
		[]PendingInterruption(nil), interrupt.Continuation.PendingInterruptions...,
	)
	tampered.Continuation.PendingInterruptions[1].ToolName = "second-but-tampered"

	_, err = ContinueCodeModeApproval(context.Background(), tampered,
		ApprovalResponse{ApprovalID: tampered.InterruptID, Approved: true}, tools, options, nil)
	var protoErr *ProtocolError
	if !errors.As(err, &protoErr) {
		t.Fatalf("expected *ProtocolError (invalid signature), got %#v (%v)", err, err)
	}
}

// MaxInFlightBridgeRequests now bounds how many calls may accumulate in one
// pending-interruption batch (see the package doc's "Host tool bridge
// dispatch" section) -- exceeding it fails the whole invocation with
// *BridgeLimitError instead of silently growing the batch without bound.
func TestConcurrentApprovalBatch_ExceedsMaxInFlightBridgeRequests(t *testing.T) {
	tools := ToolSet{
		"guarded": {
			Name:          "guarded",
			Parameters:    map[string]interface{}{"type": "object"},
			NeedsApproval: true,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return "should not run", nil
			},
		},
	}
	js := `
		return await Promise.all([
			tools.guarded({}),
			tools.guarded({}),
			tools.guarded({}),
		]);
	`
	options := &Options{
		Approval:        &ApprovalOptions{Mode: ApprovalModeInterrupt},
		ExecutionPolicy: &ExecutionPolicy{MaxInFlightBridgeRequests: 2},
	}

	_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	var limitErr *BridgeLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected *BridgeLimitError, got %#v (%v)", err, err)
	}
}

// A script that awaits a promise tied to no host call at all (not a
// deliberately-unresolved interrupt, just ordinary JavaScript that never
// settles anything) must fail with a clear protocol error instead of
// hanging until the execution timeout -- driveCodeModeExecution's
// defensive fallback once the job queue is quiescent with nothing pending
// and nothing fatal recorded (run_code_mode.go).
func TestConcurrentApprovalBatch_AwaitingAnUnrelatedNeverSettlingPromiseFailsFast(t *testing.T) {
	start := time.Now()
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await new Promise(() => {});",
		Tools:   ToolSet{},
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{TimeoutMs: 30_000}},
	})
	elapsed := time.Since(start)
	var protoErr *ProtocolError
	if !errors.As(err, &protoErr) {
		t.Fatalf("expected *ProtocolError, got %#v (%v)", err, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("expected the quiescence check to fail fast well under the 30s timeout, took %s", elapsed)
	}
}
