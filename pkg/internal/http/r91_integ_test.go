package http

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TS handleFetchError: an abort is returned unchanged; a failed connection
// (the "Cannot connect to API" error) and an HTTP status error are not.
func TestR91integNoResponseError(t *testing.T) {
	canceled := fmt.Errorf("Post x: %w", context.Canceled)
	if got, ok := NoResponseError(canceled); !ok || got != canceled {
		t.Fatalf("canceled: %v %v", got, ok)
	}
	if _, ok := NoResponseError(fmt.Errorf("x: %w", context.DeadlineExceeded)); !ok {
		t.Fatal("deadline not reported")
	}
	if _, ok := NoResponseError(TransportError(errors.New("dial tcp: connection refused"))); ok {
		t.Fatal("connect error must keep the APICallError path")
	}
	if _, ok := NoResponseError(fmt.Errorf("%w: %w", &HTTPStatusError{StatusCode: 499}, context.Canceled)); ok {
		t.Fatal("an HTTP status error is a response")
	}
	if _, ok := NoResponseError(nil); ok {
		t.Fatal("nil")
	}
}
