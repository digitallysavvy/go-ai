package polling

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPollForCompletion_Success(t *testing.T) {
	attempts := 0
	checker := func(ctx context.Context) (*JobResult, error) {
		attempts++
		if attempts >= 3 {
			return &JobResult{
				Status:    JobStatusCompleted,
				OutputURL: "https://example.com/output.mp4",
			}, nil
		}
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: attempts * 30,
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs: 10, // Fast polling for tests
		PollTimeoutMs:  1000,
	}

	ctx := context.Background()
	result, err := PollForCompletion(ctx, checker, opts)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != JobStatusCompleted {
		t.Errorf("expected status completed, got: %v", result.Status)
	}

	if result.OutputURL != "https://example.com/output.mp4" {
		t.Errorf("expected output URL, got: %v", result.OutputURL)
	}

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got: %d", attempts)
	}
}

func TestPollForCompletion_Failure(t *testing.T) {
	checker := func(ctx context.Context) (*JobResult, error) {
		return &JobResult{
			Status: JobStatusFailed,
			Error:  "processing failed",
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  1000,
	}

	ctx := context.Background()
	_, err := PollForCompletion(ctx, checker, opts)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "job failed: processing failed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPollForCompletion_Timeout(t *testing.T) {
	checker := func(ctx context.Context) (*JobResult, error) {
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: 50,
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  50, // Very short timeout
	}

	ctx := context.Background()
	_, err := PollForCompletion(ctx, checker, opts)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestPollForCompletion_BoundsInFlightCheckerCall is a regression test for a
// bug where PollTimeoutMs was only checked *between* checker calls, with the
// raw caller ctx (no derived deadline) passed straight through to checker.
// A single slow/hanging status call could block the whole poll loop
// arbitrarily far past the configured timeout. The fix derives a
// context.WithCancel(ctx) bound to the timeout and passes it to checker, so
// an in-flight call is aborted at the deadline.
func TestPollForCompletion_BoundsInFlightCheckerCall(t *testing.T) {
	checker := func(ctx context.Context) (*JobResult, error) {
		select {
		case <-time.After(1 * time.Second):
			return &JobResult{Status: JobStatusProcessing}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  100,
	}

	start := time.Now()
	_, err := PollForCompletion(context.Background(), checker, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("PollForCompletion took %v to return after a 100ms timeout with a 1s-hanging checker call (expected well under 500ms)", elapsed)
	}
}

func TestPollForCompletion_ContextCancellation(t *testing.T) {
	checker := func(ctx context.Context) (*JobResult, error) {
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: 50,
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  5000,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := PollForCompletion(ctx, checker, opts)

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestPollForCompletion_StatusCheckError(t *testing.T) {
	checker := func(ctx context.Context) (*JobResult, error) {
		return nil, errors.New("network error")
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  1000,
	}

	ctx := context.Background()
	_, err := PollForCompletion(ctx, checker, opts)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPollForCompletion_MaxAttempts(t *testing.T) {
	attempts := 0
	checker := func(ctx context.Context) (*JobResult, error) {
		attempts++
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: attempts * 10,
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  5000,
		MaxAttempts:    5,
	}

	ctx := context.Background()
	_, err := PollForCompletion(ctx, checker, opts)

	if err == nil {
		t.Fatal("expected max attempts error, got nil")
	}

	if attempts <= 5 {
		t.Logf("attempts: %d", attempts)
	}
}

func TestPollForCompletion_ExponentialBackoff(t *testing.T) {
	attempts := 0
	checker := func(ctx context.Context) (*JobResult, error) {
		attempts++
		if attempts >= 3 {
			return &JobResult{
				Status:    JobStatusCompleted,
				OutputURL: "https://example.com/output.mp4",
			}, nil
		}
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: attempts * 30,
		}, nil
	}

	opts := PollOptions{
		PollIntervalMs:    10,
		PollTimeoutMs:     1000,
		BackoffMultiplier: 2.0, // Double interval each time
		MaxIntervalMs:     100,
	}

	ctx := context.Background()
	result, err := PollForCompletion(ctx, checker, opts)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != JobStatusCompleted {
		t.Errorf("expected status completed, got: %v", result.Status)
	}
}

func TestPollWithProgressCallback(t *testing.T) {
	progressUpdates := []int{}
	attempts := 0

	checker := func(ctx context.Context) (*JobResult, error) {
		attempts++
		progress := attempts * 25
		if attempts >= 4 {
			return &JobResult{
				Status:    JobStatusCompleted,
				Progress:  100,
				OutputURL: "https://example.com/output.mp4",
			}, nil
		}
		return &JobResult{
			Status:   JobStatusProcessing,
			Progress: progress,
		}, nil
	}

	progressCallback := func(progress int, status JobStatus) {
		progressUpdates = append(progressUpdates, progress)
	}

	opts := PollOptions{
		PollIntervalMs: 10,
		PollTimeoutMs:  1000,
	}

	ctx := context.Background()
	result, err := PollWithProgressCallback(ctx, checker, opts, progressCallback)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != JobStatusCompleted {
		t.Errorf("expected status completed, got: %v", result.Status)
	}

	if len(progressUpdates) < 4 {
		t.Errorf("expected at least 4 progress updates, got: %d", len(progressUpdates))
	}
}

func TestDefaultPollOptions(t *testing.T) {
	opts := DefaultPollOptions()

	if opts.PollIntervalMs != 2000 {
		t.Errorf("expected PollIntervalMs 2000, got: %d", opts.PollIntervalMs)
	}

	if opts.PollTimeoutMs != 300000 {
		t.Errorf("expected PollTimeoutMs 300000, got: %d", opts.PollTimeoutMs)
	}

	if opts.BackoffMultiplier != 1.0 {
		t.Errorf("expected BackoffMultiplier 1.0, got: %f", opts.BackoffMultiplier)
	}
}
