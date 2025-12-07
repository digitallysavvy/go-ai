package retry

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Config contains configuration for retry logic
type Config struct {
	// Maximum number of retry attempts (default: 3)
	MaxRetries int

	// Initial delay between retries (default: 1 second)
	InitialDelay time.Duration

	// Maximum delay between retries (default: 60 seconds)
	MaxDelay time.Duration

	// Backoff multiplier (default: 2 for exponential backoff)
	Multiplier float64

	// Jitter adds randomness to delays to prevent thundering herd (default: true)
	Jitter bool

	// ShouldRetry determines if an error should trigger a retry
	// If nil, all errors trigger retries
	ShouldRetry func(error) bool
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
		ShouldRetry:  nil, // Retry all errors by default
	}
}

// RetryFunc represents a function that can be retried
type RetryFunc func(ctx context.Context) error

// Do executes a function with retry logic using exponential backoff
func Do(ctx context.Context, cfg Config, fn RetryFunc) error {
	// Use default config if not provided
	if cfg.MaxRetries == 0 {
		cfg = DefaultConfig()
	}

	var lastErr error
	attempt := 0

	for attempt <= cfg.MaxRetries {
		// Check if context is cancelled before attempting
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("context cancelled after %d attempts: %w", attempt, lastErr)
			}
			return ctx.Err()
		default:
		}

		// Execute the function
		err := fn(ctx)
		if err == nil {
			return nil // Success!
		}

		lastErr = err
		attempt++

		// Check if we should retry this error
		if cfg.ShouldRetry != nil && !cfg.ShouldRetry(err) {
			return fmt.Errorf("non-retryable error after %d attempts: %w", attempt, err)
		}

		// If we've exhausted retries, return the error
		if attempt > cfg.MaxRetries {
			return fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxRetries, err)
		}

		// Calculate delay with exponential backoff
		delay := calculateDelay(attempt, cfg)

		// Wait before retrying
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("context cancelled after %d attempts: %w", attempt, lastErr)
		case <-timer.C:
			// Continue to next attempt
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxRetries, lastErr)
}

// calculateDelay calculates the delay for the given attempt using exponential backoff
func calculateDelay(attempt int, cfg Config) time.Duration {
	// Calculate exponential backoff: initialDelay * (multiplier ^ (attempt - 1))
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Add jitter if enabled (random 0-25% variation)
	if cfg.Jitter {
		jitter := delay * 0.25 * (0.5 + (float64(time.Now().UnixNano()%1000) / 2000.0))
		delay = delay + jitter
	}

	return time.Duration(delay)
}

// WithExponentialBackoff is a convenience function that uses default exponential backoff config
func WithExponentialBackoff(ctx context.Context, fn RetryFunc) error {
	return Do(ctx, DefaultConfig(), fn)
}

// WithCustomBackoff allows specifying custom retry parameters
func WithCustomBackoff(ctx context.Context, maxRetries int, initialDelay, maxDelay time.Duration, fn RetryFunc) error {
	cfg := Config{
		MaxRetries:   maxRetries,
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   2.0,
		Jitter:       true,
		ShouldRetry:  nil,
	}
	return Do(ctx, cfg, fn)
}

// IsRetryable is a helper to check if an error should be retried
// Can be used as the ShouldRetry function in Config
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// TODO: Add more sophisticated retry logic based on error types
	// For now, retry all errors except context cancellation
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	return true
}
