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
