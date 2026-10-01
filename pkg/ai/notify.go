package ai

import (
	"context"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Listener is a function that receives an event of type E.
type Listener[E any] func(ctx context.Context, event E)

// Notify safely dispatches event to every listener in listeners concurrently,
// mirroring the TypeScript SDK's `notify()` (Promise.all over the callbacks).
//
// Each listener runs in its own goroutine. If a listener panics, the panic is
// recovered and silently discarded so that other listeners still run and the
// caller's control flow is never interrupted. Notify blocks until every
// listener has returned (or panicked).
//
// Passing a nil slice or an empty slice is valid and is a no-op.
func Notify[E any](ctx context.Context, event E, listeners ...Listener[E]) {
	publishDiagnosticForCallbackEvent(ctx, event)

	var wg sync.WaitGroup
	for _, fn := range listeners {
		if fn == nil {
			continue
		}
		wg.Add(1)
		go func(fn Listener[E]) {
			defer wg.Done()
			safeCall(ctx, event, fn)
		}(fn)
	}
	wg.Wait()
}

func publishDiagnosticForCallbackEvent[E any](ctx context.Context, event E) {
	switch e := any(event).(type) {
	case ObjectOnStartEvent:
		if e.IsEnabled != nil && !*e.IsEnabled {
			return
		}
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnStart, e)
	case ObjectOnStepStartEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnObjectStepStart, e)
	case ObjectOnStepFinishEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnObjectStepFinish, e)
	case ObjectOnFinishEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnFinish, e)
	}
}

// safeCall invokes fn(ctx, event) and recovers from any panic.
func safeCall[E any](ctx context.Context, event E, fn Listener[E]) {
	defer func() {
		recover() //nolint:errcheck // intentionally ignore panic value
	}()
	fn(ctx, event)
}

// safeInvoke calls fn() and recovers from any panic, so a panicking
// synchronous callback (e.g. StreamText/StreamObject's OnChunk or OnError)
// cannot kill the stream-processing goroutine mid-stream (audit row 9a37469
// / WG5). Unlike Notify/safeCall, this does not run fn concurrently: OnChunk
// and OnError must observe chunks/errors in stream order.
func safeInvoke(fn func()) {
	defer func() {
		recover() //nolint:errcheck // intentionally ignore panic value
	}()
	fn()
}

// safeInvokeBool is safeInvoke for a fn that returns a bool (e.g.
// StreamTextOptions.OnErrorRetry): a panic is treated as "false" (no retry
// requested) rather than propagating.
func safeInvokeBool(fn func() bool) (result bool) {
	defer func() {
		if recover() != nil { //nolint:errcheck // intentionally ignore panic value
			result = false
		}
	}()
	return fn()
}
