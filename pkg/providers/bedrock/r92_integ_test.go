package bedrock

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
)

type r92integBedrockDialTimeout struct{}

func (r92integBedrockDialTimeout) Error() string        { return "i/o timeout" }
func (r92integBedrockDialTimeout) Timeout() bool        { return true }
func (r92integBedrockDialTimeout) Temporary() bool      { return true }
func (r92integBedrockDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

// TestR92integProviderErrorFromTransportDialTimeout (cr-r92-carry-to-provider-owners):
// the shared bedrock transport mapper classifies with the request's context
// before anything else, so a dial timeout while ctx is live is the retryable
// "Cannot connect to API" error (TS handle-fetch-error.ts ETIMEDOUT /
// UND_ERR_CONNECT_TIMEOUT), while the caller's own cancel or deadline is
// returned unchanged.
func TestR92integProviderErrorFromTransportDialTimeout(t *testing.T) {
	dialErr := &url.Error{Op: "Post", URL: "https://bedrock-runtime.us-east-1.amazonaws.com/model/m/converse", Err: &net.OpError{Op: "dial", Net: "tcp", Err: r92integBedrockDialTimeout{}}}

	got := providerErrorFromTransport(context.Background(), dialErr)
	r, ok := got.(interface{ IsRetryable() bool })
	if got == nil || !strings.Contains(got.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("live ctx dial timeout = %T %v, want the retryable Cannot connect to API error", got, got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := &url.Error{Op: "Post", URL: dialErr.URL, Err: context.Canceled}
	if got := providerErrorFromTransport(ctx, canceled); got != error(canceled) {
		t.Fatalf("caller cancel = %v, want it unchanged", got)
	}

	dctx, dcancel := context.WithTimeout(context.Background(), 0)
	defer dcancel()
	<-dctx.Done()
	if got := providerErrorFromTransport(dctx, dialErr); got != error(dialErr) || !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("caller deadline = %v, want it unchanged", got)
	}
}
