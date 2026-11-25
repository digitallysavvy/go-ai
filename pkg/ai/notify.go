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

// callbackTelemetryKey carries the effective telemetry settings of an object
// call to Notify, so the callback events it publishes on the diagnostic
// channel are filtered by the recording flags.
type callbackTelemetryKey struct{}

// withCallbackTelemetry returns ctx carrying settings for
// publishDiagnosticForCallbackEvent.
func withCallbackTelemetry(ctx context.Context, settings *telemetry.Options) context.Context {
	return context.WithValue(ctx, callbackTelemetryKey{}, settings)
}

// publishDiagnosticForCallbackEvent publishes an object callback event on the
// `ai:telemetry` diagnostic channel. TS filters every channel message by the
// recording flags (create-telemetry-dispatcher.ts augmentEvent ->
// filterTelemetryEvent, 8da48db187), so subscribers get a filtered copy;
// the listeners keep the full event.
func publishDiagnosticForCallbackEvent[E any](ctx context.Context, event E) {
	settings, _ := ctx.Value(callbackTelemetryKey{}).(*telemetry.Options)
	switch e := any(event).(type) {
	case ObjectOnStartEvent:
		if e.IsEnabled != nil && !*e.IsEnabled {
			return
		}
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnStart, telemetry.FilterEvent(e, settings))
	case ObjectOnStepStartEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnObjectStepStart, telemetry.FilterEvent(e, settings))
	case ObjectOnStepFinishEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnObjectStepFinish, telemetry.FilterEvent(e, settings))
	case ObjectOnFinishEvent:
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnFinish, telemetry.FilterEvent(e, settings))
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
