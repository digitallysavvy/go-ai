package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Round 91 (stream).

// core1-A01 (TS set-abort-timeout.ts:27-36 labels any non-null timeoutMs;
// merge-abort-signals.ts:17 AbortSignal.timeout(0)): a configured zero or
// negative timeout has the TS text, not the Go fallback.
func TestR91streamZeroTimeoutText(t *testing.T) {
	zero, neg := time.Duration(0), -5*time.Millisecond
	tc := &TimeoutConfig{PerStep: &zero, FirstChunk: &zero, PerChunk: &neg}
	for reason, want := range map[TimeoutReason]string{
		TimeoutReasonStep:       "Step timeout of 0ms exceeded",
		TimeoutReasonFirstChunk: "First chunk timeout of 0ms exceeded",
		TimeoutReasonChunk:      "Chunk timeout of -5ms exceeded",
	} {
		if got := tc.timeoutError(reason, context.DeadlineExceeded).Error(); got != want {
			t.Errorf("%s: %q, want %q", reason, got, want)
		}
	}
	if got := NewToolTimeoutError(0, context.DeadlineExceeded).Error(); got != "The operation was aborted due to timeout" {
		t.Errorf("tool 0: %q", got)
	}
	// An unconfigured duration keeps the Go text.
	if got := (&TimeoutError{Reason: TimeoutReasonStep, Err: context.DeadlineExceeded}).Error(); got != "step timeout exceeded: context deadline exceeded" {
		t.Errorf("unset: %q", got)
	}

	// StreamText with PerStep 0 on a blocking stream: the abort reason.
	var mu sync.Mutex
	var reason error
	model := r90streamModel(func(ctx context.Context) provider.TextStream {
		return &r90streamBlocking{ctx: ctx}
	})
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "go", Timeout: &TimeoutConfig{PerStep: &zero},
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) { mu.Lock(); reason = e.Reason; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	r91streamDrain(res.FullStream())
	mu.Lock()
	defer mu.Unlock()
	if reason == nil || reason.Error() != "Step timeout of 0ms exceeded" {
		t.Fatalf("abort reason = %v", reason)
	}
}

func r91streamDrain(s provider.TextStream) []provider.StreamChunk {
	var out []provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			return out
		}
		out = append(out, *c)
	}
}
