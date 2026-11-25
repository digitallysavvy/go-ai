package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TS delay() rejects with DOMException('Delay was aborted', 'AbortError')
// whatever the signal's reason is (provider-utils/src/delay.ts), so an abort
// during the backoff wait is the context's own error, not a custom cause.
func TestR95carryAbortDuringDelayIsNotTheCause(t *testing.T) {
	cause := errors.New("r95carry custom reason")
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel(cause) }()
	err := Do(ctx, Config{MaxRetries: 3, InitialDelay: time.Hour, Multiplier: 1}, func(context.Context) error {
		return errors.New("boom")
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, cause) {
		t.Fatalf("err = %v, want context.Canceled (not the cause)", err)
	}
}
