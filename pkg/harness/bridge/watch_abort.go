package bridge

import "context"

// WatchAbort blocks until the caller's ctx is cancelled or the turn settles
// (done closes). On cancellation it calls onAbort, unless the turn already
// settled: like TS `onAbort`, a late abort frame must not be sent because it
// would hit the bridge's next turn. When both channels are ready Go's select
// picks at random (a caller doing `defer cancel()` right after the turn ends),
// so the settled check after the select is what makes the outcome
// deterministic. Run it in its own goroutine.
func WatchAbort(ctx context.Context, done <-chan struct{}, onAbort func()) {
	select {
	case <-ctx.Done():
		select {
		case <-done:
			return
		default:
		}
		onAbort()
	case <-done:
	}
}
