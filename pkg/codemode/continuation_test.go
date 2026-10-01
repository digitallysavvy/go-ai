package codemode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TypeScript's code-mode/src/run-compatibility.test.ts ("run
// compatibility adapter"), the two continuation/interrupt cases not already
// covered by run_code_mode_test.go's "supports tool names that are not
// host-function identifiers".

// "applies maxToolOutputBytes to interrupt resolutions".
func TestContinueCodeModeInterrupt_MaxToolOutputBytesAppliesToResolution(t *testing.T) {
	tools := ToolSet{"authorize": {
		Name:       "authorize",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			resume, _ := opts.CodeModeInterrupt.(*InterruptExecutionContext)
			if resume == nil {
				return nil, RequestCodeModeInterrupt(InterruptPayload{"kind": "authorization"})
			}
			return resume.Resolution, nil
		},
	}}
	options := &Options{ExecutionPolicy: &ExecutionPolicy{MaxToolOutputBytes: 8}}

	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.authorize({});",
		Tools:   tools,
		Options: options,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt, ok := pending.(*Interrupt)
	if !ok {
		t.Fatalf("expected *Interrupt, got %#v", pending)
	}

	_, err = ContinueCodeModeInterrupt(context.Background(), *interrupt, map[string]interface{}{"authorized": true}, tools, options, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "exceeds the 8 byte size limit") {
		t.Fatalf("error %q does not mention the byte size limit", got)
	}
}

// "accepts legacy short continuation signing keys".
func TestContinueCodeModeInterrupt_AcceptsShortSigningKey(t *testing.T) {
	tools := ToolSet{"authorize": {
		Name:       "authorize",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			resume, _ := opts.CodeModeInterrupt.(*InterruptExecutionContext)
			if resume == nil {
				return nil, RequestCodeModeInterrupt(InterruptPayload{"kind": "authorization"})
			}
			return resume.Resolution, nil
		},
	}}
	options := &Options{ContinuationSecurity: &ContinuationSecurityOptions{SigningKey: []byte("legacy-key")}}

	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.authorize({});",
		Tools:   tools,
		Options: options,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeInterrupt(pending, *options.ContinuationSecurity) {
		t.Fatalf("expected a valid interrupt, got %#v", pending)
	}
	interrupt := pending.(*Interrupt)

	got, err := ContinueCodeModeInterrupt(context.Background(), *interrupt, map[string]interface{}{"authorized": true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"authorized": true})
}

// Ports approval-continuation.test.ts's "supports generic host
// interruptions" (the non-approval RequestCodeModeInterrupt path).
func TestContinueCodeModeInterrupt_GenericHostInterruption(t *testing.T) {
	tools := ToolSet{"connection": {
		Name:       "connection",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			resume, _ := opts.CodeModeInterrupt.(*InterruptExecutionContext)
			if resume == nil {
				return nil, RequestCodeModeInterrupt(InterruptPayload{"kind": "connection-auth"})
			}
			return resume.Resolution, nil
		},
	}}

	pending, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.connection({});", Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeInterrupt(pending) {
		t.Fatalf("expected a valid interrupt, got %#v", pending)
	}
	interrupt := pending.(*Interrupt)
	if interrupt.Payload.Kind() != "connection-auth" {
		t.Fatalf("unexpected payload kind: %#v", interrupt.Payload)
	}

	got, err := ContinueCodeModeInterrupt(context.Background(), *interrupt, map[string]interface{}{"token": "ready"}, tools, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"token": "ready"})
}

// New coverage for this Go port's replay/detached-bridge-request behavior,
// not present as a standalone TypeScript test (TypeScript's equivalent
// scenario lives in the underlying `run` package, which this port does not
// vendor -- see the package doc's "Deterministic replay" section and
// errors.go's DetachedBridgeRequestError doc comment for why it is
// reachable here specifically through a caught-and-suppressed
// interruption).
// A try/catch around `await tools.guarded({})` does not change anything:
// since CM3, an approval-needing dispatch's Promise is never rejected or
// thrown (see toolBridge.pendingBatch's doc comment) -- it is left
// deliberately pending, exactly as TypeScript's context.interrupt(payload)
// never settles its own Promise either -- so `await` just suspends the
// script normally until the interrupt is resolved; there is nothing here
// for a catch block to ever observe. This documents that the behavior is
// an ordinary pending interrupt, not an error, with or without the
// try/catch (this test predates CM3's genuine concurrent-dispatch bridge,
// when the previous synchronous-unwind implementation made the catch
// block observe a real JS exception -- see
// TestRunCodeMode_DetachedBridgeRequestForAnUnawaitedInterruptedCall below
// for the scenario *DetachedBridgeRequestError is actually for).
func TestRunCodeMode_TryCatchAroundAnInterruptDoesNotObserveAnything(t *testing.T) {
	executed := false
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "should not run yet", nil
		},
	}}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS: `
			try {
				return await tools.guarded({});
			} catch (e) {
				return 'caught';
			}
		`,
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsCodeModeApprovalInterrupt(pending) {
		t.Fatalf("expected a pending approval interrupt, got %#v", pending)
	}
	if executed {
		t.Fatal("the guarded tool must not execute before approval")
	}
}

// *DetachedBridgeRequestError is for a call whose Promise is never even
// observed at all -- a fire-and-forget `tools.guarded({});` with no
// `await`/`.then()`/anything -- so the script can complete (or, here,
// return a value having nothing to do with it) while the interruption it
// raised is left dangling, unresolved, forever. Mirrors TypeScript's
// RunDetachedBridgeRequestError, produced by `run`'s own
// __runAssertNoDetachedBridgeCalls (an unawaited bridge Promise).
func TestRunCodeMode_DetachedBridgeRequestForAnUnawaitedInterruptedCall(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "should not run", nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS: `
			tools.guarded({});
			return 'done';
		`,
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	var detached *DetachedBridgeRequestError
	if !errors.As(err, &detached) {
		t.Fatalf("expected *DetachedBridgeRequestError, got %#v (%v)", err, err)
	}
}

// Genuinely sequential `await`s (as opposed to a concurrent
// `Promise.all([...])`, see
// TestContinueCodeModeApproval_PromiseAllBatchesBothCallsTogether in
// approval_continuation_test.go) never dispatch "second" until "first"'s
// interruption is resolved and real execution resumes past it -- the
// sandbox's job queue is quiescent with only "first" pending the moment
// `await tools.first({})` is reached, so this correctly produces two
// single-item continuations chained together, exactly as TypeScript's own
// genuinely sequential dispatch does (TypeScript's batching, too, only
// ever collects calls dispatched within the same synchronous burst).
func TestContinueCodeModeApproval_ChainsTwoSequentialApprovals(t *testing.T) {
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
		const first = await tools.first({});
		const second = await tools.second({});
		return { first, second };
	`
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}
	toolExecOpts := &types.ToolExecutionOptions{ToolCallID: "outer"}

	pendingFirst, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options, ToolExecutionOptions: toolExecOpts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstInterrupt, ok := pendingFirst.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(firstInterrupt) || firstInterrupt.ToolName != "first" {
		t.Fatalf("expected a pending approval interrupt for 'first', got %#v", pendingFirst)
	}
	if firstCalls != 0 || secondCalls != 0 {
		t.Fatalf("neither tool should have executed yet: first=%d second=%d", firstCalls, secondCalls)
	}

	pendingSecond, err := ContinueCodeModeApproval(context.Background(), *firstInterrupt, ApprovalResponse{ApprovalID: firstInterrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secondInterrupt, ok := pendingSecond.(*Interrupt)
	if !ok || !IsCodeModeApprovalInterrupt(secondInterrupt) || secondInterrupt.ToolName != "second" {
		t.Fatalf("expected a pending approval interrupt for 'second', got %#v", pendingSecond)
	}
	if firstCalls != 1 {
		t.Fatalf("'first' should have executed exactly once by now, got %d", firstCalls)
	}
	if secondCalls != 0 {
		t.Fatalf("'second' should not have executed yet, got %d", secondCalls)
	}

	final, err := ContinueCodeModeApproval(context.Background(), *secondInterrupt, ApprovalResponse{ApprovalID: secondInterrupt.InterruptID, Approved: true}, tools, options, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, final, map[string]interface{}{"first": "first", "second": "second"})
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("each tool should have executed exactly once: first=%d second=%d", firstCalls, secondCalls)
	}
}

// Exercises prepareContinuation/toolBridge's generic handling of a
// multi-entry PendingInterruptions batch -- the shape this package itself
// never produces (see the package doc's "Host tool bridge dispatch"
// section: it only ever emits single-entry batches), but that the doc
// claims the consuming code still handles correctly for "a hypothetical
// future concurrent implementation, or, structurally, one from
// TypeScript". This hand-builds a signed two-entry Continuation the way
// such an implementation would, then drives it through the same
// ContinueCodeModeInterrupt/RunCodeMode path a real caller would use:
// resolving entry 0 must yield exactly entry 1 as the next Interrupt
// (not skip it or execute early), and only resolving entry 1 too must
// replay the (empty) ledger, resume both calls with their respective
// resolutions via ToolExecutionOptions.CodeModeInterrupt, and complete.
func TestContinueCodeModeInterrupt_ResumesHandBuiltTwoEntryBatch(t *testing.T) {
	const outerToolCall = "outer"
	tools := ToolSet{
		"a": {
			Name:       "a",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				resume, _ := opts.CodeModeInterrupt.(*InterruptExecutionContext)
				if resume == nil {
					t.Fatal("tool 'a' executed without resume context")
				}
				return resume.Resolution, nil
			},
		},
		"b": {
			Name:       "b",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				resume, _ := opts.CodeModeInterrupt.(*InterruptExecutionContext)
				if resume == nil {
					t.Fatal("tool 'b' executed without resume context")
				}
				return resume.Resolution, nil
			},
		},
	}
	js := `
		const a = await tools.a({});
		const b = await tools.b({});
		return { a, b };
	`

	emptyLedger, err := encodeReplayLedger(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unsigned := Continuation{
		Version:         2,
		JS:              js,
		OuterToolCallID: outerToolCall,
		ToolNames:       []string{"a", "b"},
		Token:           emptyLedger,
		PendingInterruptions: []PendingInterruption{
			{
				RunInterruptionID: "interrupt-1",
				InterruptID:       outerToolCall + ":tool-1:interrupt",
				ToolName:          "a",
				ToolCallID:        outerToolCall + ":tool-1",
				Input:             map[string]interface{}{},
				Payload:           InterruptPayload{"kind": "generic"},
			},
			{
				RunInterruptionID: "interrupt-2",
				InterruptID:       outerToolCall + ":tool-2:interrupt",
				ToolName:          "b",
				ToolCallID:        outerToolCall + ":tool-2",
				Input:             map[string]interface{}{},
				Payload:           InterruptPayload{"kind": "generic"},
			},
		},
		Resolutions: []PendingResolution{},
	}
	security, err := resolveContinuationSecurity(ContinuationSecurityOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	signed, err := signContinuation(unsigned, security)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstInterrupt, err := toCodeModeInterrupt(signed, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pendingSecond, err := ContinueCodeModeInterrupt(context.Background(), *firstInterrupt, "resolved-a", tools, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secondInterrupt, ok := pendingSecond.(*Interrupt)
	if !ok {
		t.Fatalf("expected the second batch entry as a *Interrupt, got %#v", pendingSecond)
	}
	if secondInterrupt.ToolName != "b" || secondInterrupt.InterruptID != unsigned.PendingInterruptions[1].InterruptID {
		t.Fatalf("expected entry 1 ('b') next, got %#v", secondInterrupt)
	}
	if len(secondInterrupt.Continuation.Resolutions) != 1 {
		t.Fatalf("expected exactly one recorded resolution, got %#v", secondInterrupt.Continuation.Resolutions)
	}

	final, err := ContinueCodeModeInterrupt(context.Background(), *secondInterrupt, "resolved-b", tools, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, final, map[string]interface{}{"a": "resolved-a", "b": "resolved-b"})
}

// No TypeScript test exercises getCodeModeInterrupt/unwrapCodeModeResult
// directly (grep of code-mode/src/*.test.ts confirms neither name appears
// outside the implementation files), so this is new coverage for the
// nested toolResults/content unwrapping their TypeScript doc comments
// describe, exercised through the two shapes RunCodeMode's own callers are
// likely to hand back: the interrupt itself, and one wrapped a level down
// inside a generic tool-result batch.
func TestUnwrapCodeModeResult(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.guarded({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt := pending.(*Interrupt)

	direct := UnwrapCodeModeResult(interrupt)
	if direct.Status != "interrupted" || direct.Interrupt == nil || direct.Interrupt.InterruptID != interrupt.InterruptID {
		t.Fatalf("expected the direct interrupt to unwrap as interrupted, got %#v", direct)
	}

	wrapped := map[string]interface{}{
		"toolResults": []interface{}{
			map[string]interface{}{
				"output": map[string]interface{}{"type": "json", "value": interrupt},
			},
		},
	}
	nested := UnwrapCodeModeResult(wrapped)
	if nested.Status != "interrupted" || nested.Interrupt == nil || nested.Interrupt.InterruptID != interrupt.InterruptID {
		t.Fatalf("expected the nested interrupt to unwrap as interrupted, got %#v", nested)
	}

	completed := UnwrapCodeModeResult(map[string]interface{}{"answer": float64(42)})
	if completed.Status != "completed" || completed.Interrupt != nil {
		t.Fatalf("expected a completed result, got %#v", completed)
	}
	assertDeepEqual(t, completed.Output, map[string]interface{}{"answer": float64(42)})
}

func TestSetCodeModeContinuationSigningKey_RejectsEmptyKey(t *testing.T) {
	if err := SetCodeModeContinuationSigningKey([]byte{}, 0); err == nil {
		t.Fatal("expected an error for an empty signing key")
	}
}

func TestSetCodeModeContinuationSigningKey_RejectsNonPositiveMaxAge(t *testing.T) {
	if err := SetCodeModeContinuationSigningKey([]byte("a-signing-key"), -1); err == nil {
		t.Fatal("expected an error for a non-positive maxAge")
	}
}

// Byte-for-byte cross-implementation fixture: the exact HMAC-SHA256
// signature TypeScript's signContinuationPayload/canonicalJson
// (continuation-capability.ts) compute for a fixed envelope and key,
// captured by independently running that algorithm under Node against the
// same inputs (see the parity review notes). Guards against regressions
// like the one this fixture caught: tagging ContinuationAuth.Signature
// without `omitempty` made Go's canonical payload include `"signature":""`
// where TypeScript's omits the key entirely (via destructuring), which
// silently produced HMACs TypeScript never signs or verifies even though
// every Go-only round trip (sign then verify) stayed internally
// consistent and so never caught it.
func TestSignContinuationPayload_MatchesTypeScriptFixture(t *testing.T) {
	continuation := Continuation{
		Version:         2,
		JS:              "return 1;",
		OuterToolCallID: "x",
		ToolNames:       []string{"a"},
		Token:           "tok",
		PendingInterruptions: []PendingInterruption{{
			RunInterruptionID: "r1",
			InterruptID:       "i1",
			ToolName:          "a",
			ToolCallID:        "c1",
			Input:             map[string]interface{}{},
			Payload:           InterruptPayload{"kind": "k"},
		}},
		Resolutions: []PendingResolution{},
		Auth: ContinuationAuth{
			Alg:         signatureAlgorithm,
			Nonce:       strings.Repeat("n", 32),
			IssuedAtMs:  1000,
			ExpiresAtMs: 2000,
		},
	}
	got, err := signContinuationPayload(continuation, []byte("key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Computed by running TypeScript's own canonicalJson + createHmac('sha256',
	// 'key').update(json).digest('base64url') under Node against the
	// identical continuation/auth object above (auth built without ever
	// setting a "signature" property, matching signContinuationPayload's
	// `{ ...auth }` where auth: Omit<CodeModeContinuationAuth,'signature'>).
	const wantTypeScriptSignature = "2t9zRLwGhJ40AqlofAb377pYH8KGKtqk4aR-C7DS-1I"
	if got != wantTypeScriptSignature {
		t.Fatalf("signature mismatch with TypeScript fixture:\n got:  %s\n want: %s", got, wantTypeScriptSignature)
	}
}

// A tampered continuation (any field of the signed envelope changed after
// signing) must fail verification, so IsCodeModeInterrupt/
// ContinueCodeModeInterrupt reject it even though it is otherwise
// well-formed. Not a literal TypeScript test, but the direct behavioral
// consequence of continuation-capability.ts's HMAC signing, which this Go
// port ports byte-for-byte (see continuation.go's canonicalJSON doc).
func TestVerifyCodeModeContinuation_RejectsTamperedEnvelope(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}}
	pending, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.guarded({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt := pending.(*Interrupt)
	if !IsCodeModeInterrupt(interrupt) {
		t.Fatal("expected the freshly minted interrupt to be valid")
	}

	tampered := *interrupt
	tampered.ToolName = "different"
	if IsCodeModeInterrupt(&tampered) {
		t.Fatal("expected a tampered interrupt to be rejected")
	}

	tamperedContinuation := *interrupt
	tamperedContinuation.Continuation.OuterToolCallID = "forged"
	if IsCodeModeInterrupt(&tamperedContinuation) {
		t.Fatal("expected an interrupt with a tampered continuation to be rejected")
	}
}
