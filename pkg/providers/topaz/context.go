package topaz

import "context"

// mergeAbortContext derives a context that is canceled when either ctx is
// done or abortSignal (the provider.ImageGenerateOptions.AbortSignal /
// provider.VideoModelV3CallOptions.AbortSignal field, a second,
// TS-shaped way for a direct caller to signal cancellation) is done.
// pkg/ai's GenerateImage/GenerateVideo wrappers set AbortSignal to the same
// ctx they pass as the first argument, so in the common path this is a
// no-op; it only matters for a caller that sets AbortSignal directly without
// also canceling ctx (mirroring the TS SDK, where abortSignal is the only
// cancellation channel). The returned cancel func is non-nil and always
// safe to call; callers should defer it to release the AfterFunc stop and
// derived context resources.
func mergeAbortContext(ctx context.Context, abortSignal context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if abortSignal == nil {
		return context.WithCancel(ctx)
	}

	callCtx, cancel := context.WithCancel(ctx)
	select {
	case <-abortSignal.Done():
		cancel()
		return callCtx, cancel
	default:
	}

	stop := context.AfterFunc(abortSignal, cancel)
	return callCtx, func() {
		stop()
		cancel()
	}
}
