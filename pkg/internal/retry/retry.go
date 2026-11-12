package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsdate"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// Config contains configuration for retry logic
type Config struct {
	// Maximum number of retry attempts (default: 3)
	MaxRetries int

	// Initial delay between retries (default: 1 second)
	InitialDelay time.Duration

	// Maximum delay between retries (default: 60 seconds via DefaultConfig;
	// zero or negative means no cap, as TS retryWithExponentialBackoff has
	// none)
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

func newRetryError(reason providererrors.RetryErrorReason, errors []error) *providererrors.RetryError {
	copied := append([]error(nil), errors...)
	last := copied[len(copied)-1]
	message := fmt.Sprintf("Failed after %d attempts. Last error: %s", len(copied), last.Error())
	if reason == providererrors.RetryReasonErrorNotRetryable {
		// TS: `with non-retryable error: '${errorMessage}'`. Escape embedded
		// single quotes so the quoted span stays unambiguous; the text is
		// identical to TS whenever the message has none.
		message = fmt.Sprintf("Failed after %d attempts with non-retryable error: '%s'", len(copied), strings.ReplaceAll(last.Error(), "'", `\'`))
	}
	return &providererrors.RetryError{
		Message:   message,
		Reason:    reason,
		LastError: last,
		Errors:    copied,
	}
}

// Do executes a function with retry logic using exponential backoff
func Do(ctx context.Context, cfg Config, fn RetryFunc) error {
	// Use default config if not provided
	if cfg.MaxRetries == 0 {
		cfg = DefaultConfig()
	}

	var lastErr error
	errs := []error{}
	attempt := 0

	for attempt <= cfg.MaxRetries {
		// Check if context is cancelled before attempting
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
		}

		// Execute the function
		err := fn(ctx)
		if err == nil {
			return nil // Success!
		}

		// An aborted call is never retried and never wrapped (TS rethrows
		// abort errors as is, even after earlier failed attempts).
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return err
		}

		lastErr = err
		errs = append(errs, err)
		attempt++

		// If we've exhausted retries, return the error. TS checks this before
		// shouldRetry, so a non-retryable error on the last allowed attempt
		// still reports maxRetriesExceeded.
		if attempt > cfg.MaxRetries {
			return newRetryError(providererrors.RetryReasonMaxRetriesExceeded, errs)
		}

		// Check if we should retry this error
		if cfg.ShouldRetry != nil && !cfg.ShouldRetry(err) {
			if attempt == 1 {
				return err
			}
			return newRetryError(providererrors.RetryReasonErrorNotRetryable, errs)
		}

		// Calculate delay with exponential backoff, respecting retry headers
		// from provider errors when present.
		delay := calculateDelay(attempt, cfg, err)

		// Wait before retrying
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			// TS delay() rejects with the abort reason, not the last error.
			return context.Cause(ctx)
		case <-timer.C:
			// Continue to next attempt
		}
	}

	if len(errs) > 0 {
		return newRetryError(providererrors.RetryReasonMaxRetriesExceeded, errs)
	}
	return lastErr
}

// calculateDelay calculates the delay for the given attempt using exponential backoff
func calculateDelay(attempt int, cfg Config, err error) time.Duration {
	// Calculate exponential backoff: initialDelay * (multiplier ^ (attempt - 1))
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Cap at max delay
	if cfg.MaxDelay > 0 && delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Add jitter if enabled (random 0-25% variation)
	if cfg.Jitter {
		jitter := delay * 0.25 * (0.5 + (float64(time.Now().UnixNano()%1000) / 2000.0))
		delay = delay + jitter
	}

	exponentialDelay := time.Duration(delay)
	if headerDelay, ok := retryHeaderDelay(err, exponentialDelay); ok {
		return headerDelay
	}
	return exponentialDelay
}

func retryHeaderDelay(err error, exponentialDelay time.Duration) (time.Duration, bool) {
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) || len(providerErr.ResponseHeaders) == 0 {
		return 0, false
	}

	var retryAfterMS string
	var retryAfter string
	for name, value := range providerErr.ResponseHeaders {
		switch strings.ToLower(name) {
		case "retry-after-ms":
			retryAfterMS = value
		case "retry-after":
			retryAfter = value
		}
	}

	// TS retry-with-exponential-backoff.ts getRetryDelayInMs: parseFloat of
	// retry-after-ms, then parseFloat of retry-after (seconds), else
	// Date.parse(retryAfter) - Date.now().
	var delay time.Duration
	var hasDelay bool
	if retryAfterMS != "" {
		if parsed, ok := jsParseFloat(retryAfterMS); ok {
			delay = time.Duration(parsed * float64(time.Millisecond))
			hasDelay = true
		}
	}
	if retryAfter != "" && !hasDelay {
		if parsed, ok := jsParseFloat(retryAfter); ok {
			delay = time.Duration(parsed * float64(time.Second))
			hasDelay = true
		} else if when, ok := jsdate.Parse(retryAfter); ok {
			delay = time.Duration(when-time.Now().UnixMilli()) * time.Millisecond
			hasDelay = true
		}
	}
	if !hasDelay {
		return 0, false
	}
	if delay >= 0 && (delay < 60*time.Second || delay < exponentialDelay) {
		return delay, true
	}
	return 0, false
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

	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	var gatewayErr gatewayerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.IsRetryable()
	}

	var providerErr *providererrors.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.IsRetryable()
	}

	// A bare HTTP error response (not wrapped in a ProviderError) follows the
	// TS APICallError rule: only 408, 409, 429 and 5xx are retryable.
	var httpErr providererrors.HTTPStatusDetails
	if errors.As(err, &httpErr) {
		code, _, _ := httpErr.HTTPStatusDetails()
		if code != 0 {
			return code == 408 || code == 409 || code == 429 || code >= 500
		}
	}

	return true
}

// jsParseFloat is JS parseFloat: leading JS whitespace is skipped and the
// longest StrDecimalLiteral prefix ("Infinity" included) is read; false
// where parseFloat returns NaN.
func jsParseFloat(s string) (float64, bool) {
	s = strings.TrimLeftFunc(s, isJSWhiteSpace)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if s[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	digits := func(j int) int {
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		return j
	}
	start := i
	i = digits(i)
	intDigits := i > start
	fracDigits := false
	if i < len(s) && s[i] == '.' {
		j := digits(i + 1)
		fracDigits = j > i+1
		if intDigits || fracDigits {
			i = j
		}
	}
	if !intDigits && !fracDigits {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if k := digits(j); k > j {
			i = k
		}
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		// Only a range error is possible here: JS gives +-Infinity or 0.
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return f, true
		}
		return 0, false
	}
	return f, true
}

// isJSWhiteSpace is the JS WhiteSpace and LineTerminator set (String
// trim / parseFloat).
func isJSWhiteSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}
