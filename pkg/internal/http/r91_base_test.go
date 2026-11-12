package http

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type r91timeoutErr struct{}

func (r91timeoutErr) Error() string   { return "i/o timeout" }
func (r91timeoutErr) Timeout() bool   { return true }
func (r91timeoutErr) Temporary() bool { return true }

// B1 (OVC-02): TransportError is the exported transportError behavior.
func TestR91baseTransportError(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("wrap: %w", context.Canceled), r91timeoutErr{}} {
		if got := TransportError(err); got != err {
			t.Fatalf("TransportError(%v) = %v, want unchanged", err, got)
		}
	}

	rej := &FetchRejection{Err: errors.New("no credentials")}
	if got := TransportError(fmt.Errorf("Post x: %w", rej)); got != rej {
		t.Fatalf("FetchRejection not returned as is: %v", got)
	}

	cause := errors.New("dial tcp: connection refused")
	got := TransportError(cause)
	if got.Error() != "Cannot connect to API: dial tcp: connection refused" {
		t.Fatalf("text = %q", got.Error())
	}
	if !errors.Is(got, cause) {
		t.Fatal("cause not wrapped")
	}
	r, ok := got.(interface{ IsRetryable() bool })
	if !ok || !r.IsRetryable() {
		t.Fatal("connect error not retryable")
	}
	if transportError(cause).Error() != got.Error() {
		t.Fatal("transportError does not delegate")
	}
}
