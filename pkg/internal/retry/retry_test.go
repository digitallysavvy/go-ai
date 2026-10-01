package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
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
	persistentErr := errors.New("persistent error")
	cfg := Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}

	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return persistentErr
	})

	if err == nil {
		t.Fatal("expected error")
	}
	var retryErr *providererrors.RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("error = %T, want *RetryError", err)
	}
	if retryErr.Reason != providererrors.RetryReasonMaxRetriesExceeded {
		t.Fatalf("retry reason = %s, want %s", retryErr.Reason, providererrors.RetryReasonMaxRetriesExceeded)
	}
	if retryErr.LastError != persistentErr || len(retryErr.Errors) != 4 {
		t.Fatalf("retry error = %#v, want 4 errors and last persistent error", retryErr)
	}
	if !errors.Is(err, persistentErr) {
		t.Fatalf("errors.Is did not match persistent error")
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

func TestDo_NonRetryableAfterRetryReturnsRetryError(t *testing.T) {
	t.Parallel()

	retryableErr := errors.New("retryable")
	nonRetryableErr := errors.New("non-retryable")
	calls := 0

	cfg := Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, nonRetryableErr)
		},
	}

	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return retryableErr
		}
		return nonRetryableErr
	})

	var retryErr *providererrors.RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("error = %T, want *RetryError", err)
	}
	if retryErr.Reason != providererrors.RetryReasonErrorNotRetryable {
		t.Fatalf("retry reason = %s, want %s", retryErr.Reason, providererrors.RetryReasonErrorNotRetryable)
	}
	if retryErr.LastError != nonRetryableErr || len(retryErr.Errors) != 2 {
		t.Fatalf("retry error = %#v, want 2 errors and last non-retryable error", retryErr)
	}
	if !errors.Is(err, nonRetryableErr) {
		t.Fatalf("errors.Is did not match non-retryable error")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
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

	delay1 := calculateDelay(1, cfg, nil)
	delay2 := calculateDelay(2, cfg, nil)
	delay3 := calculateDelay(3, cfg, nil)

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

	delay := calculateDelay(5, cfg, nil)

	// Should be capped at max delay
	if delay > 600*time.Millisecond {
		t.Errorf("delay should be capped at ~500ms, got %v", delay)
	}
}

func TestCalculateDelay_RespectsRetryAfterHeaders(t *testing.T) {
	t.Parallel()

	cfg := Config{
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}

	err := &providererrors.ProviderError{
		Message: "rate limited",
		ResponseHeaders: map[string]string{
			"retry-after-ms": "5000",
		},
	}
	if delay := calculateDelay(1, cfg, err); delay != 5*time.Second {
		t.Fatalf("delay = %v, want retry-after-ms 5s", delay)
	}
}

func TestCalculateDelay_RetryAfterSecondsAndBounds(t *testing.T) {
	t.Parallel()

	cfg := Config{
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}

	shortRetryAfter := &providererrors.ProviderError{
		ResponseHeaders: map[string]string{"retry-after": "30"},
	}
	if delay := calculateDelay(1, cfg, shortRetryAfter); delay != 30*time.Second {
		t.Fatalf("delay = %v, want retry-after 30s", delay)
	}

	tooLongRetryAfter := &providererrors.ProviderError{
		ResponseHeaders: map[string]string{"retry-after-ms": "70000"},
	}
	if delay := calculateDelay(1, cfg, tooLongRetryAfter); delay != 2*time.Second {
		t.Fatalf("delay = %v, want exponential fallback 2s", delay)
	}
}

func TestCalculateDelay_PrefersRetryAfterMS(t *testing.T) {
	t.Parallel()

	cfg := Config{
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}
	err := &providererrors.ProviderError{
		ResponseHeaders: map[string]string{
			"retry-after-ms": "3000",
			"retry-after":    "10",
		},
	}
	if delay := calculateDelay(1, cfg, err); delay != 3*time.Second {
		t.Fatalf("delay = %v, want retry-after-ms 3s", delay)
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
		2,                   // maxRetries
		1*time.Millisecond,  // initialDelay
		10*time.Millisecond, // maxDelay
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

func TestIsRetryable_GatewayErrorStatusCodes(t *testing.T) {
	t.Parallel()

	if !IsRetryable(gatewayerrors.NewGatewayRateLimitError("", 429, nil, "")) {
		t.Fatal("gateway 429 should be retryable")
	}
	if !IsRetryable(gatewayerrors.NewGatewayInternalServerError("", 503, nil, "")) {
		t.Fatal("gateway 503 should be retryable")
	}
	if IsRetryable(gatewayerrors.NewGatewayInvalidRequestError("", 400, nil, "")) {
		t.Fatal("gateway 400 should not be retryable")
	}
	if IsRetryable(gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")) {
		t.Fatal("gateway 401 should not be retryable")
	}
}

func TestIsRetryable_ProviderErrorStatusCodes(t *testing.T) {
	t.Parallel()

	if !IsRetryable(providererrors.NewProviderError("quiverai", 429, "", "rate limited", nil)) {
		t.Fatal("provider 429 should be retryable")
	}
	if !IsRetryable(providererrors.NewProviderError("quiverai", 503, "", "unavailable", nil)) {
		t.Fatal("provider 503 should be retryable")
	}
	if !IsRetryable(providererrors.NewProviderError("quiverai", 0, "", "network error", nil)) {
		t.Fatal("provider status 0 should preserve generic retryable behavior")
	}
	if IsRetryable(providererrors.NewProviderError("quiverai", 400, "", "bad request", nil)) {
		t.Fatal("provider 400 should not be retryable")
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
