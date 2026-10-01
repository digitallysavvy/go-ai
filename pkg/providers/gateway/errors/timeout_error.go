package errors

import (
	"context"
	"errors"
	"fmt"
	"net"
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

	// GenerationID identifies the failed gateway generation when available.
	GenerationID string

	// RawType, Code, and Param preserve Gateway response payload details.
	RawType string
	Code    interface{}
	Param   interface{}
}

// Error implements the error interface
func (e *GatewayTimeoutError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Duration > 0 {
		return fmt.Sprintf("Gateway request timed out after %v", e.Duration)
	}
	return "Request timed out"
}

// Unwrap returns the underlying cause
func (e *GatewayTimeoutError) Unwrap() error {
	return e.Cause
}

func (e *GatewayTimeoutError) GatewayErrorMarker() {}

func (e *GatewayTimeoutError) GetStatusCode() int { return e.StatusCode }

func (e *GatewayTimeoutError) GetType() string { return "timeout_error" }

func (e *GatewayTimeoutError) GetGenerationID() string { return e.GenerationID }

func (e *GatewayTimeoutError) GetRawType() string { return e.RawType }

func (e *GatewayTimeoutError) GetCode() interface{} { return e.Code }

func (e *GatewayTimeoutError) GetParam() interface{} { return e.Param }

func (e *GatewayTimeoutError) IsRetryable() bool {
	return e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusConflict || e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= http.StatusInternalServerError
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

    This is a client-side timeout. To resolve this, increase your timeout configuration: https://vercel.com/docs/ai-gateway/capabilities/video-generation#extending-timeouts-for-node.js`,
		originalMessage)

	return &GatewayTimeoutError{
		Message:    message,
		Cause:      cause,
		Provider:   "gateway",
		StatusCode: http.StatusRequestTimeout,
	}
}

// IsTimeoutError checks for structured timeout signals.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	if IsGatewayTimeoutError(err) {
		return true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
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
