package retry

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS retryWithExponentialBackoff: `Failed after ${tryNumber} attempts with
// non-retryable error: '${errorMessage}'`, the message as is.
func TestR94carryNonRetryableMessageIsVerbatim(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{MaxRetries: 2, ShouldRetry: func(error) bool { return calls == 1 }}, func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("busy")
		}
		return errors.New("can't do 'that'")
	})
	var retryErr *providererrors.RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("err = %v, want a RetryError", err)
	}
	if want := "Failed after 2 attempts with non-retryable error: 'can't do 'that''"; retryErr.Message != want {
		t.Fatalf("message = %q, want %q", retryErr.Message, want)
	}
}
