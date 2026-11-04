package codemode

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var r87ccarryObject = map[string]interface{}{"type": "object"}

// r87ccarryDetachedTools is the tool set of TS exceptions.test.ts
// "detached tool calls" (TS 3fab50aa9b, line 86).
func r87ccarryDetachedTools() ToolSet {
	return ToolSet{
		"createSlide": {Name: "createSlide", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			}},
		"fail": {Name: "fail", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("tool exploded")
			}},
		"update-slide": {Name: "update-slide", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				time.Sleep(50 * time.Millisecond)
				return map[string]interface{}{"ok": true}, nil
			}},
		"slide$&": {Name: "slide$&", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			}},
	}
}

func r87ccarryDetached(t *testing.T, js string, tools ToolSet) *DetachedBridgeRequestError {
	t.Helper()
	_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
	var detached *DetachedBridgeRequestError
	if !errors.As(err, &detached) {
		t.Fatalf("err = %#v (%v), want *DetachedBridgeRequestError", err, err)
	}
	return detached
}

// exceptions.test.ts 110: names the tool for unawaited calls (ordinary
// tools, no approval).
func TestR87ccarryDetachedNamesTheToolForUnawaitedCalls(t *testing.T) {
	err := r87ccarryDetached(t, "tools.createSlide({}); return 1;", r87ccarryDetachedTools())
	if got, want := err.Error(), "JavaScript runtime created an unawaited hostFunction bridge request: tools.createSlide."; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// exceptions.test.ts 122: names the tool for calls still pending on return.
func TestR87ccarryDetachedNamesTheToolForCallsStillPendingOnReturn(t *testing.T) {
	err := r87ccarryDetached(t,
		"try { await Promise.all([tools.fail({}), tools['update-slide']({})]); } catch {} return 1;",
		r87ccarryDetachedTools())
	if got, want := err.Error(), `JavaScript runtime returned while a hostFunction bridge request was still pending: tools["update-slide"].`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// exceptions.test.ts 134: inserts tool names containing $ patterns
// literally.
func TestR87ccarryDetachedInsertsDollarPatternsLiterally(t *testing.T) {
	err := r87ccarryDetached(t, "tools['slide$&']({}); return 1;", r87ccarryDetachedTools())
	if got, want := err.Error(), `JavaScript runtime created an unawaited hostFunction bridge request: tools["slide$&"].`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// run's records: an unobserved call never reaches the host, the details are
// the record (id in creation order, the TS host function name, the status),
// and an unknown tool keeps the `__codeMode.missing` host function name.
func TestR87ccarryDetachedRecordDetails(t *testing.T) {
	var runs atomic.Int32
	tools := r87ccarryDetachedTools()
	tools["createSlide"] = types.Tool{Name: "createSlide", Parameters: r87ccarryObject,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			runs.Add(1)
			return 1, nil
		}}

	err := r87ccarryDetached(t, "await tools.fail({}).catch(() => {}); tools.createSlide({}); return 1;", tools)
	assertDeepEqual(t, err.Details, map[string]interface{}{"id": 2, "kind": "hostFunction", "name": "__codeMode.tool0", "status": "idle"})
	if runs.Load() != 0 {
		t.Fatalf("an unobserved call ran %d times", runs.Load())
	}

	err = r87ccarryDetached(t, "tools.nope({}); return 1;", tools)
	if got, want := err.Error(), "JavaScript runtime created an unawaited hostFunction bridge request: __codeMode.missing."; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	assertDeepEqual(t, err.Details, map[string]interface{}{"id": 1, "kind": "hostFunction", "name": "__codeMode.missing", "status": "idle"})

	// Observed with .then but not awaited: pending when the script returns.
	err = r87ccarryDetached(t, "tools['update-slide']({}).then(() => {}); return 1;", tools)
	if got, want := err.Error(), `JavaScript runtime returned while a hostFunction bridge request was still pending: tools["update-slide"].`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	assertDeepEqual(t, err.Details, map[string]interface{}{"id": 1, "kind": "hostFunction", "name": "__codeMode.tool3", "status": "pending"})
}

// A detached call does not hide the script's own failure: run checks for
// detached calls only when it serializes a result.
func TestR87ccarryDetachedCheckOnlyOnSuccess(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:    "tools.createSlide({}); throw new Error('script failed');",
		Tools: r87ccarryDetachedTools(),
	})
	var detached *DetachedBridgeRequestError
	if err == nil || errors.As(err, &detached) || err.Error()[:len("Error: script failed")] != "Error: script failed" {
		t.Fatalf("err = %#v (%v), want the script's error", err, err)
	}
}

// Host calls run concurrently, as in TS: both calls of a Promise.all are in
// flight together (each waits for the other to start).
func TestR87ccarryHostCallsRunConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	both := make(chan struct{})
	go func() { wg.Wait(); close(both) }()
	wait := func(name string) types.Tool {
		return types.Tool{Name: name, Parameters: r87ccarryObject,
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				wg.Done()
				select {
				case <-both:
					return name, nil
				case <-time.After(5 * time.Second):
					return nil, errors.New("the other call never started")
				}
			}}
	}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await Promise.all([tools.a({}), tools.b({})]);",
		Tools: ToolSet{"a": wait("a"), "b": wait("b")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDeepEqual(t, got, []interface{}{"a", "b"})
}

// TS run's in-flight limit counts calls running at once.
func TestR87ccarryInFlightLimitCountsRunningCalls(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "tools.echo({}).then(() => {}); return await tools.echo({});",
		Tools:   echoTools(),
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxInFlightBridgeRequests: 1}},
	})
	var limit *BridgeLimitError
	if !errors.As(err, &limit) || limit.Error() != "JavaScript runtime exceeded the 1 in-flight bridge request limit." {
		t.Fatalf("err = %#v (%v)", err, err)
	}
}

// r87ccarryApproveAll runs js in interrupt mode and approves every pending
// interruption until the script finishes.
func r87ccarryApproveAll(t *testing.T, js string, tools ToolSet) interface{} {
	t.Helper()
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}
	result, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	for i := 0; err == nil && i < 10; i++ {
		interrupt, ok := result.(*Interrupt)
		if !ok {
			return result
		}
		result, err = ContinueCodeModeApproval(context.Background(), *interrupt,
			ApprovalResponse{ApprovalID: interrupt.InterruptID, Approved: true}, tools, options, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("still interrupted: %#v", result)
	return nil
}

func r87ccarryGuarded() types.Tool {
	return types.Tool{Name: "guarded", Parameters: r87ccarryObject, NeedsApproval: true,
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return input, nil
		}}
}

// A resumed run delivers replayed results in their recorded settlement
// order (TS run's queueBridgeResponse), so a script that depends on which
// call settled first replays the same way, and settled calls do not run
// again.
func TestR87ccarryReplayKeepsSettlementOrder(t *testing.T) {
	var slowRuns, fastRuns atomic.Int32
	tools := ToolSet{
		"slow": {Name: "slow", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				slowRuns.Add(1)
				time.Sleep(60 * time.Millisecond)
				return "slow", nil
			}},
		"fast": {Name: "fast", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				fastRuns.Add(1)
				return "fast", nil
			}},
		"guarded": r87ccarryGuarded(),
	}
	result := r87ccarryApproveAll(t, `
		const order = [];
		await Promise.all([
			tools.slow({}).then(v => order.push(v)),
			tools.fast({}).then(v => order.push(v)),
		]);
		return await tools.guarded({ order });
	`, tools)
	assertDeepEqual(t, result, map[string]interface{}{"order": []interface{}{"fast", "slow"}})
	if slowRuns.Load() != 1 || fastRuns.Load() != 1 {
		t.Fatalf("runs: slow %d, fast %d; want 1 each", slowRuns.Load(), fastRuns.Load())
	}
}

// TS run records a failed call in the ledger and replays the rejection on
// resume instead of running the tool again.
func TestR87ccarryReplayRejectedCalls(t *testing.T) {
	var failRuns atomic.Int32
	tools := ToolSet{
		"fail": {Name: "fail", Parameters: r87ccarryObject,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				failRuns.Add(1)
				return nil, NewToolError("fail broke", nil)
			}},
		"guarded": r87ccarryGuarded(),
	}
	result := r87ccarryApproveAll(t, `
		let failure = '';
		try { await tools.fail({}); } catch (e) { failure = e.message; }
		return await tools.guarded({ failure });
	`, tools)
	assertDeepEqual(t, result, map[string]interface{}{"failure": "fail broke"})
	if failRuns.Load() != 1 {
		t.Fatalf("fail ran %d times, want 1", failRuns.Load())
	}
}

// r87ccarryResumeTampered runs js to its approval interrupt, lets tamper
// change the continuation's replay ledger, re-signs it and resumes with the
// approval granted.
func r87ccarryResumeTampered(t *testing.T, js string, tamper func(map[int]replayRecord)) error {
	t.Helper()
	tools := ToolSet{
		"echo": echoTools()["echo"],
		"guarded": {Name: "guarded", Parameters: r87ccarryObject, NeedsApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}},
	}
	options := &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}}
	pending, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	interrupt := pending.(*Interrupt)
	ledger, err := decodeReplayLedger(interrupt.Continuation.Token)
	if err != nil {
		t.Fatal(err)
	}
	tamper(ledger)
	token, err := encodeReplayLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := interrupt.Continuation
	unsigned.Token = token
	security, err := resolveContinuationSecurity(ContinuationSecurityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signContinuation(unsigned, security)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunCodeMode(context.Background(), RunInput{
		JS: js, Tools: tools, Options: options, Continuation: &signed,
		InterruptResolution: &InterruptResolution{InterruptID: interrupt.InterruptID, Resolution: ApprovalDecision{Approved: true}},
	})
	return err
}

// A continuation recorded before settlement orders and statuses existed
// still resumes (its records replay as fulfilled, in start order).
func TestR87ccarryReplayLegacyLedger(t *testing.T) {
	err := r87ccarryResumeTampered(t, "await tools.echo({ n: 1 }); await tools.echo({ n: 2 }); return await tools.guarded({});", func(ledger map[int]replayRecord) {
		for idx, rec := range ledger {
			rec.SettledOrder = 0
			ledger[idx] = rec
		}
	})
	if err != nil {
		t.Fatalf("legacy ledger resume failed: %v", err)
	}
}
