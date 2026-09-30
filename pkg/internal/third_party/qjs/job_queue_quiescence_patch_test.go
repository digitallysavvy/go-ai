package qjs

import (
	"context"
	"testing"
)

// TestJobQueueQuiescence_RunPendingJobsDrainsWithoutHanging is a
// regression test for the vendoring patch documented in
// QJS_RunPendingJobs/QJS_EvalNoAutoAwait's Go doc comments and
// README.vendor.md's "qjs.wasm rebuild" section: pkg/codemode's concurrent
// approval batching depends on being able to drain the job queue to
// exhaustion and learn whether a promise is still pending -- crucially,
// without hanging or busy-spinning when that promise is one a host
// deliberately never resolves (an interrupt awaiting approval). This
// exercises the three settlement outcomes (fulfilled, deliberately never
// settled, rejected) directly against the rebuilt qjs.wasm, independent of
// pkg/codemode's own (more thorough) integration coverage.
func TestJobQueueQuiescence_RunPendingJobsDrainsWithoutHanging(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rt.Close()

	jsCtx := rt.Context()

	// A promise that resolves via a microtask chain (Promise.resolve().then)
	// should fully settle after RunPendingJobs, with 0 remaining runs
	// afterward.
	p, err := jsCtx.EvalNoAutoAwait("probe1.js", Code(`Promise.resolve(1).then(v => v + 41)`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !p.IsPromise() {
		t.Fatalf("expected a promise, got IsUndefined=%v IsObject=%v IsNull=%v str=%q", p.IsUndefined(), p.IsObject(), p.IsNull(), p.String())
	}
	state := p.PromiseState()
	if state != PromiseStatePending {
		t.Fatalf("expected pending before draining, got %v", state)
	}
	ran, rerr := rt.RunPendingJobs()
	if rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}
	if ran == 0 {
		t.Fatalf("expected at least one job to run")
	}
	state = p.PromiseState()
	if state != PromiseStateFulfilled {
		t.Fatalf("expected fulfilled after draining, got %v", state)
	}
	result := p.PromiseResult()
	if got := result.Int32(); got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}

	// A promise that is never settled by any JS-visible mechanism (built
	// via Promise.withResolvers and never resolved) must leave
	// RunPendingJobs returning 0 once the queue is drained -- this is the
	// exact "quiescent but still pending" case the Go host bridge relies
	// on, and it must NOT hang.
	pending, err := jsCtx.EvalNoAutoAwait("probe2.js", Code(`Promise.withResolvers().promise`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !pending.IsPromise() {
		t.Fatalf("expected a promise")
	}
	ran, rerr = rt.RunPendingJobs()
	if rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}
	if ran != 0 {
		t.Fatalf("expected 0 jobs left to run once the queue is drained, got %d", ran)
	}
	if pending.PromiseState() != PromiseStatePending {
		t.Fatalf("expected the never-settled promise to remain pending")
	}

	// A rejected promise must report PromiseStateRejected with its reason
	// available via PromiseResult.
	rej, err := jsCtx.EvalNoAutoAwait("probe3.js", Code(`Promise.reject(new Error("boom"))`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, rerr := rt.RunPendingJobs(); rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}
	if rej.PromiseState() != PromiseStateRejected {
		t.Fatalf("expected rejected")
	}
	reason := rej.PromiseResult()
	if got := reason.Exception(); got == nil || got.Error() == "" {
		t.Fatalf("expected a non-empty rejection reason, got %v", got)
	}
}

// TestJobQueueQuiescence_AsyncDispatchBurstIsSynchronous validates the
// other half of the same mechanism: binding a single async host function
// that, when called twice back-to-back inside one Promise.all([...])
// array literal, is invoked synchronously by the guest BOTH times (each
// call returning its own pending Promise immediately) before either of
// the two underlying `this.Promise()` handles is resolved/rejected from
// Go. This is what lets the Go host bridge (pkg/codemode) collect a full
// "wave" of concurrent dispatches before deciding which ones need to
// pause for approval, instead of the first one unwinding before the
// second is ever reached.
func TestJobQueueQuiescence_AsyncDispatchBurstIsSynchronous(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rt.Close()

	jsCtx := rt.Context()

	var calls []string
	var pendingPromises []*Value

	dispatch := jsCtx.Function(func(this *This) (*Value, error) {
		args := this.Args()
		name := args[0].String()
		calls = append(calls, name)
		// Deliberately do NOT resolve/reject this.Promise() here -- leave
		// it pending, exactly like an interrupt-needing tool call would.
		pendingPromises = append(pendingPromises, this.Promise())
		return nil, nil
	}, true)
	jsCtx.Global().SetPropertyStr("__dispatch", dispatch)

	top, err := jsCtx.EvalNoAutoAwait("burst.js", Code(`Promise.all([__dispatch("a"), __dispatch("b")])`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}

	// Both calls must already have happened synchronously, before we ever
	// drained the job queue.
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("expected both calls dispatched synchronously in order, got %v", calls)
	}

	ran, rerr := rt.RunPendingJobs()
	if rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}
	if ran != 0 {
		t.Fatalf("expected no jobs runnable while both dispatches are still pending, got %d", ran)
	}
	if !top.IsPromise() || top.PromiseState() != PromiseStatePending {
		t.Fatalf("expected Promise.all's own promise to still be pending")
	}

	// Now resolve both (mirrors two sibling calls that both turn out not
	// to need approval) and confirm the whole thing settles.
	for i, p := range pendingPromises {
		val := jsCtx.NewString(calls[i] + "-result")
		if rerr := p.Resolve(val); rerr != nil {
			t.Fatalf("Resolve: %v", rerr)
		}
	}
	if _, rerr := rt.RunPendingJobs(); rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}
	if top.PromiseState() != PromiseStateFulfilled {
		t.Fatalf("expected Promise.all to fulfill once both dispatches resolved")
	}
}
