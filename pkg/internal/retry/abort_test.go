package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// An abort after earlier failed attempts is returned as is, not wrapped in a
// RetryError (TS rethrows abort errors).
func TestDoReturnsAbortErrorUnwrappedAfterRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := Do(ctx, Config{MaxRetries: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1, ShouldRetry: IsRetryable}, func(c context.Context) error {
		calls++
		if calls == 1 {
			return providererrors.NewProviderError("p", 503, "", "x", nil)
		}
		cancel()
		return c.Err()
	})
	var re *providererrors.RetryError
	if errors.As(err, &re) || !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// TS checks `tryNumber > maxRetries` before shouldRetry, so a non-retryable
// error on the last allowed attempt reports maxRetriesExceeded.
func TestDoMaxRetriesExceededWinsOverNonRetryable(t *testing.T) {
	calls := 0
	retryable := errors.New("retryable")
	err := Do(context.Background(), Config{MaxRetries: 1, InitialDelay: time.Millisecond, Multiplier: 1, ShouldRetry: func(e error) bool { return e == retryable }}, func(context.Context) error {
		calls++
		if calls == 1 {
			return retryable
		}
		return errors.New("fatal")
	})
	var re *providererrors.RetryError
	if !errors.As(err, &re) || re.Reason != providererrors.RetryReasonMaxRetriesExceeded {
		t.Fatalf("err=%v", err)
	}
}

// Cancelling during the backoff delay returns the abort error (TS delay()
// rejects with the abort reason), not the last attempt's error.
func TestDoCancelDuringDelayReturnsAbortError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err := Do(ctx, Config{MaxRetries: 3, InitialDelay: time.Hour, Multiplier: 1}, func(context.Context) error {
		return errors.New("boom")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

// TS has no maximum backoff delay.
func TestCalculateDelayHasNoCapWhenMaxDelayUnset(t *testing.T) {
	d := calculateDelay(6, Config{InitialDelay: 2 * time.Second, Multiplier: 2}, errors.New("x"))
	if d != 64*time.Second {
		t.Fatalf("d=%v", d)
	}
}
