package errors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestNewGatewayTimeoutError(t *testing.T) {
	duration := 30 * time.Second
	provider := "gateway"
	message := "request timed out"
	cause := errors.New("underlying error")

	err := NewGatewayTimeoutError(duration, provider, message, cause)

	if err.Duration != duration {
		t.Errorf("Duration = %v, want %v", err.Duration, duration)
	}
	if err.Provider != provider {
		t.Errorf("Provider = %v, want %v", err.Provider, provider)
	}
	if err.Message != message {
		t.Errorf("Message = %v, want %v", err.Message, message)
	}
	if err.Cause != cause {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if err.StatusCode != http.StatusRequestTimeout {
		t.Errorf("StatusCode = %v, want %v", err.StatusCode, http.StatusRequestTimeout)
	}
}

func TestGatewayTimeoutError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *GatewayTimeoutError
		contains string
	}{
		{
			name: "with custom message",
			err: &GatewayTimeoutError{
				Message: "custom timeout message",
			},
			contains: "custom timeout message",
		},
		{
			name: "with duration",
			err: &GatewayTimeoutError{
				Duration: 30 * time.Second,
			},
			contains: "30s",
		},
		{
			name:     "default message",
			err:      &GatewayTimeoutError{},
			contains: "timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if !containsSubstring(got, tt.contains) {
				t.Errorf("Error() = %v, want to contain %v", got, tt.contains)
			}
		})
	}
}

func TestGatewayTimeoutError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &GatewayTimeoutError{
		Cause: cause,
	}

	if got := err.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
}

func TestIsGatewayTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "is gateway timeout error",
			err:  &GatewayTimeoutError{},
			want: true,
		},
		{
			name: "wrapped gateway timeout error",
			err:  fmt.Errorf("wrapped: %w", &GatewayTimeoutError{}),
			want: true,
		},
		{
			name: "not a gateway timeout error",
			err:  errors.New("some error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGatewayTimeoutError(tt.err); got != tt.want {
				t.Errorf("IsGatewayTimeoutError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateTimeoutError(t *testing.T) {
	originalMessage := "connection timeout"
	cause := errors.New("underlying cause")

	err := CreateTimeoutError(originalMessage, cause)

	if err == nil {
		t.Fatal("CreateTimeoutError() returned nil")
	}

	// Verify error contains helpful information
	errMsg := err.Error()
	if !containsSubstring(errMsg, originalMessage) {
		t.Errorf("Error message should contain original message, got: %v", errMsg)
	}
	if !containsSubstring(errMsg, "client-side timeout") {
		t.Errorf("Error message should mention client-side timeout, got: %v", errMsg)
	}
	if !containsSubstring(errMsg, "context.WithTimeout") {
		t.Errorf("Error message should contain troubleshooting guidance, got: %v", errMsg)
	}

	if err.Cause != cause {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if err.StatusCode != http.StatusRequestTimeout {
		t.Errorf("StatusCode = %v, want %v", err.StatusCode, http.StatusRequestTimeout)
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "gateway timeout error",
			err:  &GatewayTimeoutError{},
			want: true,
		},
		{
			name: "error with timeout in message",
			err:  errors.New("request timeout"),
			want: true,
		},
		{
			name: "error with timed out in message",
			err:  errors.New("connection timed out"),
			want: true,
		},
		{
			name: "error with deadline exceeded in message",
			err:  errors.New("deadline exceeded"),
			want: true,
		},
		{
			name: "error with i/o timeout",
			err:  errors.New("i/o timeout"),
			want: true,
		},
		{
			name: "wrapped context deadline exceeded",
			err:  fmt.Errorf("operation failed: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			name: "regular error",
			err:  errors.New("some error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTimeoutError(tt.err); got != tt.want {
				t.Errorf("IsTimeoutError() = %v, want %v for error: %v", got, tt.want, tt.err)
			}
		})
	}
}

func TestConvertToGatewayTimeoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		provider string
		wantType string
	}{
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			provider: "gateway",
			wantType: "*errors.GatewayTimeoutError",
		},
		{
			name:     "already gateway timeout error",
			err:      &GatewayTimeoutError{Message: "already a timeout"},
			provider: "gateway",
			wantType: "*errors.GatewayTimeoutError",
		},
		{
			name:     "timeout in message",
			err:      errors.New("request timeout"),
			provider: "gateway",
			wantType: "*errors.GatewayTimeoutError",
		},
		{
			name:     "not a timeout error",
			err:      errors.New("some other error"),
			provider: "gateway",
			wantType: "*errors.errorString",
		},
		{
			name:     "nil error",
			err:      nil,
			provider: "gateway",
			wantType: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToGatewayTimeoutError(tt.err, tt.provider)

			gotType := fmt.Sprintf("%T", result)
			if gotType != tt.wantType {
				t.Errorf("ConvertToGatewayTimeoutError() type = %v, want %v", gotType, tt.wantType)
			}

			// If we expect a GatewayTimeoutError, verify it's properly set
			if tt.wantType == "*errors.GatewayTimeoutError" {
				if !IsGatewayTimeoutError(result) {
					t.Error("Result should be a GatewayTimeoutError")
				}
			}
		})
	}
}

func TestTimeoutErrorWrapping(t *testing.T) {
	// Test that wrapped timeout errors are properly detected
	baseErr := context.DeadlineExceeded
	wrappedOnce := fmt.Errorf("level 1: %w", baseErr)
	wrappedTwice := fmt.Errorf("level 2: %w", wrappedOnce)

	if !IsTimeoutError(wrappedOnce) {
		t.Error("Should detect timeout error wrapped once")
	}

	if !IsTimeoutError(wrappedTwice) {
		t.Error("Should detect timeout error wrapped twice")
	}

	// Convert wrapped error
	converted := ConvertToGatewayTimeoutError(wrappedTwice, "gateway")
	if !IsGatewayTimeoutError(converted) {
		t.Error("Should convert wrapped timeout error to GatewayTimeoutError")
	}
}

// Helper function to check if a string contains a substring
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
		 (len(s) > len(substr) &&
		  (s[:len(substr)] == substr ||
		   s[len(s)-len(substr):] == substr ||
		   findSubstringHelper(s, substr))))
}

func findSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
