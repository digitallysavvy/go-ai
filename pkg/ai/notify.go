package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Listener is a function that receives an event of type E.
type Listener[E any] func(ctx context.Context, event E)

// Notify safely dispatches event to every listener in listeners.
//
// Each listener is called in order. If a listener panics, the panic is
// recovered and silently discarded so that subsequent listeners still run
// and the caller's control flow is never interrupted.
//
// Passing a nil slice or an empty slice is valid and is a no-op.
func Notify[E any](ctx context.Context, event E, listeners ...Listener[E]) {
	publishDiagnosticForCallbackEvent(ctx, event)
	for _, fn := range listeners {
		if fn == nil {
			continue
		}
		safeCall(ctx, event, fn)
	}
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
