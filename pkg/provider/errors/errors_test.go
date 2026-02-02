package errors

import (
	"errors"
	"testing"
)

func TestProviderError_Error(t *testing.T) {
	t.Parallel()

	err := &ProviderError{
		Provider:   "openai",
		StatusCode: 400,
		ErrorCode:  "invalid_request",
		Message:    "Bad request",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}
	if !contains(errStr, "openai") {
		t.Error("expected error to contain provider name")
	}
	if !contains(errStr, "400") {
		t.Error("expected error to contain status code")
	}
	if !contains(errStr, "invalid_request") {
		t.Error("expected error to contain error code")
	}
}

func TestProviderError_ErrorWithCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &ProviderError{
		Provider:   "openai",
		StatusCode: 500,
		ErrorCode:  "server_error",
		Message:    "Internal error",
		Cause:      cause,
	}

	errStr := err.Error()
	if !contains(errStr, "underlying error") {
		t.Error("expected error to contain cause")
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &ProviderError{
		Provider: "openai",
		Cause:    cause,
	}

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}

func TestIsProviderError(t *testing.T) {
	t.Parallel()

	providerErr := &ProviderError{Provider: "openai"}
	regularErr := errors.New("regular error")

	if !IsProviderError(providerErr) {
		t.Error("expected IsProviderError to return true for ProviderError")
	}
	if IsProviderError(regularErr) {
		t.Error("expected IsProviderError to return false for regular error")
	}
}

func TestNewProviderError(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := NewProviderError("openai", 400, "bad_request", "Bad request", cause)

	if err.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %s", err.Provider)
	}
	if err.StatusCode != 400 {
		t.Errorf("expected status code 400, got %d", err.StatusCode)
	}
	if err.ErrorCode != "bad_request" {
		t.Errorf("expected error code 'bad_request', got %s", err.ErrorCode)
	}
	if err.Message != "Bad request" {
		t.Errorf("expected message 'Bad request', got %s", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	err := &ValidationError{
		Field:   "temperature",
		Message: "must be between 0 and 2",
	}

	errStr := err.Error()
	if !contains(errStr, "temperature") {
		t.Error("expected error to contain field name")
	}
	if !contains(errStr, "must be between 0 and 2") {
		t.Error("expected error to contain message")
	}
}

func TestValidationError_ErrorWithoutField(t *testing.T) {
	t.Parallel()

	err := &ValidationError{
		Message: "validation failed",
	}

	errStr := err.Error()
	if !contains(errStr, "validation") {
		t.Error("expected error to contain message")
	}
}

func TestValidationError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &ValidationError{
		Message: "validation failed",
		Cause:   cause,
	}

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}

func TestIsValidationError(t *testing.T) {
	t.Parallel()

	validationErr := &ValidationError{Message: "error"}
	regularErr := errors.New("regular error")

	if !IsValidationError(validationErr) {
		t.Error("expected IsValidationError to return true for ValidationError")
	}
	if IsValidationError(regularErr) {
		t.Error("expected IsValidationError to return false for regular error")
	}
}

func TestNewValidationError(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := NewValidationError("field", "message", cause)

	if err.Field != "field" {
		t.Errorf("expected field 'field', got %s", err.Field)
	}
	if err.Message != "message" {
		t.Errorf("expected message 'message', got %s", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestToolExecutionError_Error(t *testing.T) {
	t.Parallel()

	err := &ToolExecutionError{
		ToolName:   "get_weather",
		ToolCallID: "call_123",
		Message:    "API failed",
	}

	errStr := err.Error()
	if !contains(errStr, "get_weather") {
		t.Error("expected error to contain tool name")
	}
	if !contains(errStr, "call_123") {
		t.Error("expected error to contain call ID")
	}
}

func TestToolExecutionError_ErrorWithoutCallID(t *testing.T) {
	t.Parallel()

	err := &ToolExecutionError{
		ToolName: "get_weather",
		Message:  "API failed",
	}

	errStr := err.Error()
	if !contains(errStr, "get_weather") {
		t.Error("expected error to contain tool name")
	}
}

func TestToolExecutionError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &ToolExecutionError{
		ToolName: "tool",
		Cause:    cause,
	}

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}

func TestIsToolExecutionError(t *testing.T) {
	t.Parallel()

	toolErr := &ToolExecutionError{ToolName: "tool"}
	regularErr := errors.New("regular error")

	if !IsToolExecutionError(toolErr) {
		t.Error("expected IsToolExecutionError to return true for ToolExecutionError")
	}
	if IsToolExecutionError(regularErr) {
		t.Error("expected IsToolExecutionError to return false for regular error")
	}
}

func TestNewToolExecutionError(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := NewToolExecutionError("tool", "call_1", "message", cause)

	if err.ToolName != "tool" {
		t.Errorf("expected tool name 'tool', got %s", err.ToolName)
	}
	if err.ToolCallID != "call_1" {
		t.Errorf("expected call ID 'call_1', got %s", err.ToolCallID)
	}
	if err.Message != "message" {
		t.Errorf("expected message 'message', got %s", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestStreamError_Error(t *testing.T) {
	t.Parallel()

	err := &StreamError{
		Message: "stream interrupted",
	}

	errStr := err.Error()
	if !contains(errStr, "stream") {
		t.Error("expected error to contain 'stream'")
	}
	if !contains(errStr, "interrupted") {
		t.Error("expected error to contain message")
	}
}

func TestStreamError_ErrorWithCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := &StreamError{
		Message: "stream failed",
		Cause:   cause,
	}

	errStr := err.Error()
	if !contains(errStr, "connection reset") {
		t.Error("expected error to contain cause")
	}
}

func TestStreamError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &StreamError{
		Message: "stream error",
		Cause:   cause,
	}

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}

func TestIsStreamError(t *testing.T) {
	t.Parallel()

	streamErr := &StreamError{Message: "error"}
	regularErr := errors.New("regular error")

	if !IsStreamError(streamErr) {
		t.Error("expected IsStreamError to return true for StreamError")
	}
	if IsStreamError(regularErr) {
		t.Error("expected IsStreamError to return false for regular error")
	}
}

func TestNewStreamError(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := NewStreamError("message", cause)

	if err.Message != "message" {
		t.Errorf("expected message 'message', got %s", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestRateLimitError_Error(t *testing.T) {
	t.Parallel()

	err := &RateLimitError{
		Provider: "openai",
		Message:  "rate limit exceeded",
	}

	errStr := err.Error()
	if !contains(errStr, "openai") {
		t.Error("expected error to contain provider")
	}
	if !contains(errStr, "rate limit") {
		t.Error("expected error to contain 'rate limit'")
	}
}

func TestRateLimitError_ErrorWithRetryAfter(t *testing.T) {
	t.Parallel()

	retryAfter := 60
	err := &RateLimitError{
		Provider:          "openai",
		RetryAfterSeconds: &retryAfter,
		Message:           "rate limit exceeded",
	}

	errStr := err.Error()
	if !contains(errStr, "60") {
		t.Error("expected error to contain retry after seconds")
	}
}

func TestRateLimitError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &RateLimitError{
		Provider: "openai",
		Cause:    cause,
	}

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}

func TestIsRateLimitError(t *testing.T) {
	t.Parallel()

	rateLimitErr := &RateLimitError{Provider: "openai"}
	regularErr := errors.New("regular error")

	if !IsRateLimitError(rateLimitErr) {
		t.Error("expected IsRateLimitError to return true for RateLimitError")
	}
	if IsRateLimitError(regularErr) {
		t.Error("expected IsRateLimitError to return false for regular error")
	}
}

func TestNewRateLimitError(t *testing.T) {
	t.Parallel()

	retryAfter := 30
	cause := errors.New("cause")
	err := NewRateLimitError("openai", "rate limited", &retryAfter, cause)

	if err.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %s", err.Provider)
	}
	if err.Message != "rate limited" {
		t.Errorf("expected message 'rate limited', got %s", err.Message)
	}
	if err.RetryAfterSeconds == nil || *err.RetryAfterSeconds != 30 {
		t.Error("expected retry after seconds to be 30")
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestValidationError_ErrorWithContext(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		Field:      "message.parts[3].data",
		EntityName: "tool_call",
		EntityID:   "call_abc123",
	}

	err := NewValidationErrorWithContext(
		map[string]string{"foo": "bar"},
		"Invalid type",
		nil,
		ctx,
	)

	errStr := err.Error()
	if !contains(errStr, "message.parts[3].data") {
		t.Error("expected error to contain field path")
	}
	if !contains(errStr, "tool_call") {
		t.Error("expected error to contain entity name")
	}
	if !contains(errStr, "call_abc123") {
		t.Error("expected error to contain entity ID")
	}
	if !contains(errStr, "Invalid type") {
		t.Error("expected error to contain message")
	}
}

func TestValidationError_ErrorWithContextFieldOnly(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		Field: "user.email",
	}

	err := NewValidationErrorWithContext(
		"invalid@",
		"Invalid email format",
		nil,
		ctx,
	)

	errStr := err.Error()
	if !contains(errStr, "user.email") {
		t.Error("expected error to contain field path")
	}
	if !contains(errStr, "Invalid email format") {
		t.Error("expected error to contain message")
	}
}

func TestValidationError_ErrorWithContextEntityOnly(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		EntityName: "message",
		EntityID:   "msg_123",
	}

	err := NewValidationErrorWithContext(
		nil,
		"Missing required field",
		nil,
		ctx,
	)

	errStr := err.Error()
	if !contains(errStr, "message") {
		t.Error("expected error to contain entity name")
	}
	if !contains(errStr, "msg_123") {
		t.Error("expected error to contain entity ID")
	}
}

func TestValidationError_ErrorWithContextNoValue(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		Field:      "settings.timeout",
		EntityName: "config",
	}

	err := NewValidationErrorWithContext(
		nil,
		"Timeout must be positive",
		nil,
		ctx,
	)

	errStr := err.Error()
	if !contains(errStr, "settings.timeout") {
		t.Error("expected error to contain field path")
	}
	if !contains(errStr, "config") {
		t.Error("expected error to contain entity name")
	}
	if !contains(errStr, "Timeout must be positive") {
		t.Error("expected error to contain message")
	}
}

func TestValidationError_ErrorBackwardCompatibility(t *testing.T) {
	t.Parallel()

	// Test that old-style ValidationError still works
	err := NewValidationError("temperature", "must be between 0 and 2", nil)

	errStr := err.Error()
	if !contains(errStr, "temperature") {
		t.Error("expected error to contain field name")
	}
	if !contains(errStr, "must be between 0 and 2") {
		t.Error("expected error to contain message")
	}
	if contains(errStr, "Type validation failed") {
		t.Error("expected old-style format, not new context format")
	}
}

func TestWrapValidationError(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying validation error")
	ctx := &ValidationContext{
		Field:      "data.items[0].name",
		EntityName: "item",
		EntityID:   "item_456",
	}

	err := WrapValidationError(map[string]interface{}{"invalid": true}, cause, ctx)

	if err.Context == nil {
		t.Error("expected context to be set")
	}
	if err.Context.Field != "data.items[0].name" {
		t.Errorf("expected field 'data.items[0].name', got %s", err.Context.Field)
	}
	if err.Context.EntityName != "item" {
		t.Errorf("expected entity name 'item', got %s", err.Context.EntityName)
	}
	if err.Context.EntityID != "item_456" {
		t.Errorf("expected entity ID 'item_456', got %s", err.Context.EntityID)
	}

	errStr := err.Error()
	if !contains(errStr, "data.items[0].name") {
		t.Error("expected error to contain field path")
	}
}

func TestWrapValidationError_PreservesExisting(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		Field:      "user.id",
		EntityName: "user",
		EntityID:   "user_789",
	}

	// Create original error with same context
	original := NewValidationErrorWithContext(
		"test_value",
		"original message",
		nil,
		ctx,
	)

	// Wrap with same value and context
	wrapped := WrapValidationError("test_value", original, ctx)

	// Should return the same error instance
	if wrapped != original {
		t.Error("expected WrapValidationError to return original error when context matches")
	}
}

func TestWrapValidationError_CreatesNewWhenDifferent(t *testing.T) {
	t.Parallel()

	ctx1 := &ValidationContext{
		Field: "field1",
	}

	ctx2 := &ValidationContext{
		Field: "field2",
	}

	original := NewValidationErrorWithContext(
		"value",
		"message",
		nil,
		ctx1,
	)

	// Wrap with different context
	wrapped := WrapValidationError("value", original, ctx2)

	// Should create a new error
	if wrapped == original {
		t.Error("expected WrapValidationError to create new error when context differs")
	}
	if wrapped.Context.Field != "field2" {
		t.Errorf("expected new error to have field 'field2', got %s", wrapped.Context.Field)
	}
}

func TestNewValidationErrorWithContext(t *testing.T) {
	t.Parallel()

	ctx := &ValidationContext{
		Field:      "config.api.endpoint",
		EntityName: "api_config",
		EntityID:   "cfg_001",
	}

	value := map[string]string{"endpoint": "invalid://"}
	err := NewValidationErrorWithContext(value, "Invalid URL scheme", nil, ctx)

	if err.Value == nil {
		t.Error("expected value to be set")
	}
	if err.Message != "Invalid URL scheme" {
		t.Errorf("expected message 'Invalid URL scheme', got %s", err.Message)
	}
	if err.Context == nil {
		t.Error("expected context to be set")
	}
	if err.Context.Field != "config.api.endpoint" {
		t.Errorf("expected field 'config.api.endpoint', got %s", err.Context.Field)
	}
	if err.Context.EntityName != "api_config" {
		t.Errorf("expected entity name 'api_config', got %s", err.Context.EntityName)
	}
	if err.Context.EntityID != "cfg_001" {
		t.Errorf("expected entity ID 'cfg_001', got %s", err.Context.EntityID)
	}
}

func TestValidationError_ErrorWithCauseAndContext(t *testing.T) {
	t.Parallel()

	cause := errors.New("schema mismatch")
	ctx := &ValidationContext{
		Field:      "response.data",
		EntityName: "api_response",
	}

	err := NewValidationErrorWithContext(
		nil,
		"Response validation failed",
		cause,
		ctx,
	)

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}

	errStr := err.Error()
	if !contains(errStr, "response.data") {
		t.Error("expected error to contain field path")
	}
	if !contains(errStr, "api_response") {
		t.Error("expected error to contain entity name")
	}
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	if ErrInvalidInput == nil {
		t.Error("ErrInvalidInput should not be nil")
	}
	if ErrModelNotFound == nil {
		t.Error("ErrModelNotFound should not be nil")
	}
	if ErrProviderNotFound == nil {
		t.Error("ErrProviderNotFound should not be nil")
	}
	if ErrToolNotFound == nil {
		t.Error("ErrToolNotFound should not be nil")
	}
	if ErrValidationFailed == nil {
		t.Error("ErrValidationFailed should not be nil")
	}
	if ErrUnsupportedFeature == nil {
		t.Error("ErrUnsupportedFeature should not be nil")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
