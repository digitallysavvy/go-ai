package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	t.Parallel()

	calls := 0
	err := Do(context.Background(), DefaultConfig(), func(ctx context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	cfg := Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}

	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	calls := 0
	cfg := Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}

	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return errors.New("persistent error")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 4 { // 1 initial + 3 retries
		t.Errorf("expected 4 calls, got %d", calls)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	cfg := Config{
		MaxRetries:   10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	calls := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, cfg, func(ctx context.Context) error {
		calls++
		return errors.New("error")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		// The error should contain context cancelled info
		if calls > 3 {
			t.Error("expected retry to stop early due to context cancellation")
		}
	}
}

func TestDo_ShouldRetryFalse(t *testing.T) {
	t.Parallel()

	nonRetryableErr := errors.New("non-retryable")
	calls := 0

	cfg := Config{
		MaxRetries:   5,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, nonRetryableErr)
		},
	}

	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return nonRetryableErr
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retries), got %d", calls)
	}
}

func TestDo_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", cfg.MaxRetries)
	}
	if cfg.InitialDelay != 1*time.Second {
		t.Errorf("expected InitialDelay 1s, got %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 60*time.Second {
		t.Errorf("expected MaxDelay 60s, got %v", cfg.MaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("expected Multiplier 2.0, got %f", cfg.Multiplier)
	}
	if !cfg.Jitter {
		t.Error("expected Jitter to be true")
	}
}

func TestDo_ZeroConfig(t *testing.T) {
	t.Parallel()

	// Zero config should use defaults
	calls := 0
	err := Do(context.Background(), Config{}, func(ctx context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestCalculateDelay_Basic(t *testing.T) {
	t.Parallel()

	cfg := Config{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}

	delay1 := calculateDelay(1, cfg)
	delay2 := calculateDelay(2, cfg)
	delay3 := calculateDelay(3, cfg)

	// Without jitter, delays should follow exponential pattern
	if delay1 < 90*time.Millisecond || delay1 > 110*time.Millisecond {
		t.Errorf("delay1 should be around 100ms, got %v", delay1)
	}
	if delay2 < 180*time.Millisecond || delay2 > 220*time.Millisecond {
		t.Errorf("delay2 should be around 200ms, got %v", delay2)
	}
	if delay3 < 360*time.Millisecond || delay3 > 440*time.Millisecond {
		t.Errorf("delay3 should be around 400ms, got %v", delay3)
	}
}

func TestCalculateDelay_MaxDelayRespected(t *testing.T) {
	t.Parallel()

	cfg := Config{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Multiplier:   10.0, // High multiplier to hit max quickly
		Jitter:       false,
	}

	delay := calculateDelay(5, cfg)

	// Should be capped at max delay
	if delay > 600*time.Millisecond {
		t.Errorf("delay should be capped at ~500ms, got %v", delay)
	}
}

func TestWithExponentialBackoff(t *testing.T) {
	t.Parallel()

	calls := 0
	err := WithExponentialBackoff(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestWithCustomBackoff(t *testing.T) {
	t.Parallel()

	calls := 0
	err := WithCustomBackoff(
		context.Background(),
		2,                       // maxRetries
		1*time.Millisecond,     // initialDelay
		10*time.Millisecond,    // maxDelay
		func(ctx context.Context) error {
			calls++
			if calls < 2 {
				return errors.New("error")
			}
			return nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestIsRetryable_NilError(t *testing.T) {
	t.Parallel()

	if IsRetryable(nil) {
		t.Error("nil error should not be retryable")
	}
}

func TestIsRetryable_ContextErrors(t *testing.T) {
	t.Parallel()

	if IsRetryable(context.Canceled) {
		t.Error("context.Canceled should not be retryable")
	}
	if IsRetryable(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should not be retryable")
	}
}

func TestIsRetryable_RegularError(t *testing.T) {
	t.Parallel()

	if !IsRetryable(errors.New("regular error")) {
		t.Error("regular errors should be retryable")
	}
}

func TestDo_ContextCancelledBeforeStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	err := Do(ctx, cfg, func(ctx context.Context) error {
		calls++
		return nil
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 0 {
		t.Errorf("expected 0 calls, got %d", calls)
	}
}
