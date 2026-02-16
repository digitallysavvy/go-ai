package errors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// GatewayTimeoutError represents a timeout error from the Gateway
// This occurs when a client-side timeout is detected before receiving a response
type GatewayTimeoutError struct {
	// Duration is the timeout duration
	Duration time.Duration

	// Provider is the gateway provider name
	Provider string

	// Message is the error message
	Message string

	// Cause is the underlying error
	Cause error

	// StatusCode is the HTTP status code (typically 408)
	StatusCode int
}

// Error implements the error interface
func (e *GatewayTimeoutError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Duration > 0 {
		return fmt.Sprintf("Gateway request timed out after %v", e.Duration)
	}
	return "Gateway request timed out"
}

// Unwrap returns the underlying cause
func (e *GatewayTimeoutError) Unwrap() error {
	return e.Cause
}

// IsGatewayTimeoutError checks if an error is a GatewayTimeoutError
func IsGatewayTimeoutError(err error) bool {
	var timeoutErr *GatewayTimeoutError
	return errors.As(err, &timeoutErr)
}

// NewGatewayTimeoutError creates a new GatewayTimeoutError
func NewGatewayTimeoutError(duration time.Duration, provider, message string, cause error) *GatewayTimeoutError {
	return &GatewayTimeoutError{
		Duration:   duration,
		Provider:   provider,
		Message:    message,
		Cause:      cause,
		StatusCode: http.StatusRequestTimeout,
	}
}

// CreateTimeoutError creates a helpful timeout error with troubleshooting guidance
func CreateTimeoutError(originalMessage string, cause error) *GatewayTimeoutError {
	message := fmt.Sprintf(`Gateway request timed out: %s

This is a client-side timeout. To resolve this:
1. Increase your context timeout: ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
2. For video generation, consider using longer timeouts (e.g., 10 minutes)
3. Check your network connectivity
4. Verify the Gateway API is accessible

For more information, see: https://vercel.com/docs/ai-gateway/capabilities/video-generation#extending-timeouts`,
		originalMessage)

	return &GatewayTimeoutError{
		Message:    message,
		Cause:      cause,
		Provider:   "gateway",
		StatusCode: http.StatusRequestTimeout,
	}
}

// IsTimeoutError checks if an error is a timeout error
// This checks for various timeout error types including:
// - context.DeadlineExceeded
// - HTTP client timeout errors
// - GatewayTimeoutError itself
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's already a GatewayTimeoutError
	if IsGatewayTimeoutError(err) {
		return true
	}

	// Check for context timeout
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for HTTP client timeout errors
	// These are common error types from net/http
	errMsg := err.Error()
	timeoutIndicators := []string{
		"context deadline exceeded",
		"timeout",
		"timed out",
		"deadline exceeded",
		"i/o timeout",
	}

	for _, indicator := range timeoutIndicators {
		if contains(errMsg, indicator) {
			return true
		}
	}

	// Check if the cause is a timeout
	unwrapped := errors.Unwrap(err)
	if unwrapped != nil && unwrapped != err {
		return IsTimeoutError(unwrapped)
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
		 (len(s) > len(substr) &&
		  (s[:len(substr)] == substr ||
		   s[len(s)-len(substr):] == substr ||
		   findSubstring(s, substr))))
}

// findSubstring is a simple substring search helper
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ConvertToGatewayTimeoutError converts an error to a GatewayTimeoutError if it's a timeout
func ConvertToGatewayTimeoutError(err error, provider string) error {
	if err == nil {
		return nil
	}

	// If it's already a GatewayTimeoutError, return it
	if IsGatewayTimeoutError(err) {
		return err
	}

	// If it's a timeout error, convert it
	if IsTimeoutError(err) {
		return CreateTimeoutError(err.Error(), err)
	}

	// Not a timeout error, return original
	return err
}
