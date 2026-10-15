package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error types for the AI SDK
var (
	// ErrInvalidInput indicates invalid input parameters
	ErrInvalidInput = errors.New("invalid input")

	// ErrModelNotFound indicates the requested model was not found
	ErrModelNotFound = errors.New("model not found")

	// ErrProviderNotFound indicates the requested provider was not found
	ErrProviderNotFound = errors.New("provider not found")

	// ErrToolNotFound indicates the requested tool was not found
	ErrToolNotFound = errors.New("tool not found")

	// ErrValidationFailed indicates schema validation failed
	ErrValidationFailed = errors.New("validation failed")

	// ErrUnsupportedFeature indicates a feature is not supported by the provider
	ErrUnsupportedFeature = errors.New("unsupported feature")
)

// ProviderError represents an error from an AI provider
type ProviderError struct {
	// Provider name
	Provider string

	// HTTP status code (if applicable)
	StatusCode int

	// Provider-specific error code
	ErrorCode string

	// Error message
	Message string

	// Underlying cause
	Cause error

	// ResponseHeaders are response headers from the failed provider call, when available.
	ResponseHeaders map[string]string

	// ResponseBody is the raw provider response body or stream error frame, when available.
	ResponseBody string

	// Data is provider-specific structured error data, when available.
	Data interface{}

	// Retryable overrides the default status-code-based retryability
	// classification in IsRetryable, when set. Providers use this for
	// errors whose HTTP status isn't in the default retryable set but that
	// TS still treats as retryable (e.g. Bedrock's modelStreamErrorException
	// at 424), or the reverse (a normally-retryable status that a provider
	// knows is not, in a given case). Hand-off: "ProviderError Retryable
	// override".
	Retryable *bool
}

// RetryErrorReason identifies why retrying stopped.
type RetryErrorReason string

const (
	// RetryReasonMaxRetriesExceeded means all retry attempts were exhausted.
	RetryReasonMaxRetriesExceeded RetryErrorReason = "maxRetriesExceeded"
	// RetryReasonErrorNotRetryable means a later attempt failed with a
	// non-retryable error after at least one retryable failure.
	RetryReasonErrorNotRetryable RetryErrorReason = "errorNotRetryable"
	// RetryReasonAbort means retrying stopped because the operation was aborted.
	RetryReasonAbort RetryErrorReason = "abort"
)

// RetryError mirrors the TypeScript AI SDK RetryError shape for callers that
// need to inspect retry failure reason and attempt history.
type RetryError struct {
	Message   string
	Reason    RetryErrorReason
	LastError error
	Errors    []error
}

func (e *RetryError) Error() string {
	return e.Message
}

func (e *RetryError) Unwrap() error {
	return e.LastError
}

// IsRetryError checks if an error is a RetryError.
func IsRetryError(err error) bool {
	var retryErr *RetryError
	return errors.As(err, &retryErr)
}

// Error implements the error interface
func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s provider error (%d): %s - %s (caused by: %v)",
			e.Provider, e.StatusCode, e.ErrorCode, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s provider error (%d): %s - %s",
		e.Provider, e.StatusCode, e.ErrorCode, e.Message)
}

// Unwrap returns the underlying cause
func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// IsRetryable reports whether the provider error represents a retryable
// condition. Unknown/no-status errors preserve the SDK's historical generic
// retry behavior, while HTTP responses follow the TypeScript SDK default:
// rate limits and transient server errors are retryable.
func (e *ProviderError) IsRetryable() bool {
	if e == nil {
		return false
	}
	if e.Retryable != nil {
		return *e.Retryable
	}
	// TS APICallError default: 408 (request timeout), 409 (conflict), 429, 5xx.
	return e.StatusCode == 0 || e.StatusCode == 408 || e.StatusCode == 409 || e.StatusCode == 429 || e.StatusCode >= 500
}

// IsProviderError checks if an error is a ProviderError
func IsProviderError(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr)
}

// HTTPStatusDetails is implemented by errors that carry a non-2xx HTTP
// response (internal/http.HTTPStatusError). NewProviderError uses it to
// populate the status code, response headers and body, mirroring the data TS
// APICallError carries.
type HTTPStatusDetails interface {
	error
	HTTPStatusDetails() (statusCode int, headers map[string]string, body []byte)
}

// NewProviderError creates a new provider error.
//
// When statusCode is 0 and the cause chain carries an HTTP error response,
// the status code, response headers and body are taken from it, and a message
// that is just the raw cause text is replaced by the message extracted from
// the JSON error body (TS failedResponseHandler behavior). This keeps
// retryability honest: a 401 must not be classified as a retryable
// no-status error.
func NewProviderError(provider string, statusCode int, errorCode, message string, cause error) *ProviderError {
	e := &ProviderError{
		Provider:   provider,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		Cause:      cause,
	}
	if statusCode != 0 || cause == nil {
		return e
	}
	var hs HTTPStatusDetails
	if !errors.As(cause, &hs) {
		return e
	}
	code, headers, body := hs.HTTPStatusDetails()
	if code == 0 {
		return e
	}
	e.StatusCode = code
	e.ResponseHeaders = headers
	e.ResponseBody = string(body)
	if len(body) > 0 {
		var data interface{}
		if json.Unmarshal(body, &data) == nil {
			e.Data = data
		}
	}
	if message == "" || message == cause.Error() || message == hs.Error() {
		if msg := ExtractAPIErrorMessage(body); msg != "" {
			e.Message = msg
		} else if text := http.StatusText(code); text != "" {
			// TS createJsonErrorResponseHandler: an empty or unparseable
			// body falls back to the response status text. The body stays
			// available on ResponseBody.
			e.Message = text
		}
	}
	return e
}

// ExtractAPIErrorMessage pulls a human-readable message out of a JSON error
// body using the shapes common to provider APIs: {"error":{"message"}},
// {"error":"msg"}, {"message"}, {"detail"}, {"errors":[{"message"}]}.
// It returns "" when no message is found.
func ExtractAPIErrorMessage(body []byte) string {
	var v map[string]interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	str := func(x interface{}) string {
		s, _ := x.(string)
		return strings.TrimSpace(s)
	}
	switch e := v["error"].(type) {
	case map[string]interface{}:
		if m := str(e["message"]); m != "" {
			return m
		}
	case string:
		if m := str(e); m != "" {
			return m
		}
	}
	if m := str(v["message"]); m != "" {
		return m
	}
	switch d := v["detail"].(type) {
	case string:
		if m := str(d); m != "" {
			return m
		}
	case []interface{}:
		if len(d) > 0 {
			if first, ok := d[0].(map[string]interface{}); ok {
				if m := str(first["msg"]); m != "" {
					return m
				}
				if m := str(first["message"]); m != "" {
					return m
				}
			}
		}
	}
	if arr, ok := v["errors"].([]interface{}); ok && len(arr) > 0 {
		if first, ok := arr[0].(map[string]interface{}); ok {
			if m := str(first["message"]); m != "" {
				return m
			}
		}
	}
	return ""
}

// ValidationContext provides detailed context for validation errors
type ValidationContext struct {
	// Field path in dot notation (e.g., "message.parts[3].data")
	Field string

	// Entity name (e.g., "tool_call", "message")
	EntityName string

	// Entity identifier (e.g., "call_abc123", message ID)
	EntityID string
}

// ValidationError represents a validation error
type ValidationError struct {
	// Field that failed validation (deprecated, use Context.Field instead)
	Field string

	// Error message
	Message string

	// Underlying cause
	Cause error

	// Validation context with field path, entity name, and entity ID
	Context *ValidationContext

	// Value that failed validation (for debugging)
	Value interface{}
}

// TypeValidationError is a compatibility alias used by TS parity docs.
type TypeValidationError = ValidationError

// InvalidArgumentError signals invalid caller-provided arguments/options.
type InvalidArgumentError struct {
	Field   string
	Message string
	Cause   error
}

func (e *InvalidArgumentError) Error() string {
	if e.Field == "" {
		return "invalid argument: " + e.Message
	}
	return fmt.Sprintf("invalid argument for %s: %s", e.Field, e.Message)
}

func (e *InvalidArgumentError) Unwrap() error { return e.Cause }

// IsInvalidArgumentError checks if an error is an InvalidArgumentError.
func IsInvalidArgumentError(err error) bool {
	var target *InvalidArgumentError
	return errors.As(err, &target)
}

// TooManyEmbeddingValuesForCallError signals that a provider embedding model
// received more values than it supports in one request.
type TooManyEmbeddingValuesForCallError struct {
	Provider             string
	ModelID              string
	MaxEmbeddingsPerCall int
	Values               []string
}

func (e *TooManyEmbeddingValuesForCallError) Error() string {
	return fmt.Sprintf("Too many values for a single embedding call. The %s model %q can only embed up to %d values per call, but %d values were provided.", e.Provider, e.ModelID, e.MaxEmbeddingsPerCall, len(e.Values))
}

// IsTooManyEmbeddingValuesForCallError checks if an error is a TooManyEmbeddingValuesForCallError.
func IsTooManyEmbeddingValuesForCallError(err error) bool {
	var target *TooManyEmbeddingValuesForCallError
	return errors.As(err, &target)
}

// UnsupportedFunctionalityError signals that the requested SDK/provider
// functionality cannot be represented by the current interface.
type UnsupportedFunctionalityError struct {
	Functionality string
	Message       string
}

func (e *UnsupportedFunctionalityError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("'%s' functionality not supported.", e.Functionality)
}

// IsUnsupportedFunctionalityError checks if an error is an UnsupportedFunctionalityError.
func IsUnsupportedFunctionalityError(err error) bool {
	var target *UnsupportedFunctionalityError
	return errors.As(err, &target)
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	var contextPrefix string

	// Use new context if available, otherwise fall back to Field for backward compatibility
	if e.Context != nil {
		contextPrefix = "Type validation failed"

		// Add field path if present
		if e.Context.Field != "" {
			contextPrefix += fmt.Sprintf(" for %s", e.Context.Field)
		}

		// Add entity information in parentheses
		if e.Context.EntityName != "" || e.Context.EntityID != "" {
			contextPrefix += " ("
			var parts []string
			if e.Context.EntityName != "" {
				parts = append(parts, e.Context.EntityName)
			}
			if e.Context.EntityID != "" {
				parts = append(parts, fmt.Sprintf("id: %q", e.Context.EntityID))
			}
			for i, part := range parts {
				if i > 0 {
					contextPrefix += ", "
				}
				contextPrefix += part
			}
			contextPrefix += ")"
		}

		contextPrefix += ":"

		// Format the full error message
		msg := contextPrefix
		if e.Value != nil {
			msg += fmt.Sprintf("\nValue: %+v.", e.Value)
		}
		if e.Message != "" {
			msg += fmt.Sprintf("\nError message: %s", e.Message)
		} else if e.Cause != nil {
			msg += fmt.Sprintf("\nError message: %v", e.Cause)
		}
		return msg
	}

	// Backward compatibility path
	if e.Field != "" {
		return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// Unwrap returns the underlying cause
func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// IsValidationError checks if an error is a ValidationError
func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

// NewValidationError creates a new validation error (backward compatible)
func NewValidationError(field, message string, cause error) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Cause:   cause,
	}
}

// NewValidationErrorWithContext creates a new validation error with context
func NewValidationErrorWithContext(value interface{}, message string, cause error, context *ValidationContext) *ValidationError {
	return &ValidationError{
		Value:   value,
		Message: message,
		Cause:   cause,
		Context: context,
	}
}

// WrapValidationError wraps an error with validation context
// Returns existing error if it's already a ValidationError with matching context
func WrapValidationError(value interface{}, cause error, context *ValidationContext) *ValidationError {
	// Check if it's already a ValidationError with matching context
	var existingErr *ValidationError
	if errors.As(cause, &existingErr) &&
		existingErr.Value == value &&
		existingErr.Context != nil &&
		context != nil &&
		existingErr.Context.Field == context.Field &&
		existingErr.Context.EntityName == context.EntityName &&
		existingErr.Context.EntityID == context.EntityID {
		return existingErr
	}

	// Create new ValidationError with context
	message := ""
	if existingErr != nil && existingErr.Message != "" {
		message = existingErr.Message
	}

	return &ValidationError{
		Value:   value,
		Message: message,
		Cause:   cause,
		Context: context,
	}
}

// ToolExecutionError represents an error during tool execution
type ToolExecutionError struct {
	// Name of the tool that failed
	ToolName string

	// Tool call ID
	ToolCallID string

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *ToolExecutionError) Error() string {
	if e.ToolCallID != "" {
		return fmt.Sprintf("tool execution error for '%s' (call ID: %s): %s",
			e.ToolName, e.ToolCallID, e.Message)
	}
	return fmt.Sprintf("tool execution error for '%s': %s", e.ToolName, e.Message)
}

// Unwrap returns the underlying cause
func (e *ToolExecutionError) Unwrap() error {
	return e.Cause
}

// IsToolExecutionError checks if an error is a ToolExecutionError
func IsToolExecutionError(err error) bool {
	var toolErr *ToolExecutionError
	return errors.As(err, &toolErr)
}

// NewToolExecutionError creates a new tool execution error
func NewToolExecutionError(toolName, toolCallID, message string, cause error) *ToolExecutionError {
	return &ToolExecutionError{
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Message:    message,
		Cause:      cause,
	}
}

// StreamError represents an error during streaming
type StreamError struct {
	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *StreamError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("stream error: %s (caused by: %v)", e.Message, e.Cause)
	}
	return fmt.Sprintf("stream error: %s", e.Message)
}

// Unwrap returns the underlying cause
func (e *StreamError) Unwrap() error {
	return e.Cause
}

// IsStreamError checks if an error is a StreamError
func IsStreamError(err error) bool {
	var streamErr *StreamError
	return errors.As(err, &streamErr)
}

// NewStreamError creates a new stream error
func NewStreamError(message string, cause error) *StreamError {
	return &StreamError{
		Message: message,
		Cause:   cause,
	}
}

// RateLimitError represents a rate limit error from a provider
type RateLimitError struct {
	// Provider name
	Provider string

	// Retry after duration in seconds (if provided by the provider)
	RetryAfterSeconds *int

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *RateLimitError) Error() string {
	if e.RetryAfterSeconds != nil {
		return fmt.Sprintf("rate limit exceeded for %s (retry after %d seconds): %s",
			e.Provider, *e.RetryAfterSeconds, e.Message)
	}
	return fmt.Sprintf("rate limit exceeded for %s: %s", e.Provider, e.Message)
}

// Unwrap returns the underlying cause
func (e *RateLimitError) Unwrap() error {
	return e.Cause
}

// IsRateLimitError checks if an error is a RateLimitError
func IsRateLimitError(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr)
}

// NewRateLimitError creates a new rate limit error
func NewRateLimitError(provider, message string, retryAfter *int, cause error) *RateLimitError {
	return &RateLimitError{
		Provider:          provider,
		RetryAfterSeconds: retryAfter,
		Message:           message,
		Cause:             cause,
	}
}

// DownloadError represents an error during file download
type DownloadError struct {
	// URL that was being downloaded
	URL string

	// HTTP status code (if applicable)
	StatusCode int

	// HTTP status text
	StatusText string

	// Error message
	Message string

	// Underlying cause
	Cause error

	// Body is a bounded prefix of the response body for non-2xx HTTP
	// responses, when captured. Callers that poll a JSON status endpoint
	// (e.g. fileutil.PollJSON) rather than download a binary file need this
	// to decode a provider's structured error envelope; plain file/media
	// downloads leave it nil. Nil also when the error never reached an HTTP
	// response (network/validation errors).
	Body []byte

	// Headers are the response headers for non-2xx HTTP responses, when
	// captured. See Body.
	Headers map[string][]string
}

// SSRFError represents a blocked URL due to SSRF protections.
type SSRFError struct {
	// URL that was rejected.
	URL string

	// Reason explains which SSRF rule triggered the block.
	Reason string

	// Underlying cause, if any.
	Cause error
}

// Error implements the error interface.
func (e *SSRFError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	if e.Cause != nil {
		return fmt.Sprintf("url %s blocked by SSRF protection: %v", e.URL, e.Cause)
	}
	return fmt.Sprintf("url %s blocked by SSRF protection", e.URL)
}

// Unwrap returns the underlying cause.
func (e *SSRFError) Unwrap() error {
	return e.Cause
}

// IsSSRFError checks if an error is an SSRFError.
func IsSSRFError(err error) bool {
	var ssrfErr *SSRFError
	return errors.As(err, &ssrfErr)
}

// NewSSRFError creates a new SSRF error.
func NewSSRFError(url, reason string, cause error) *SSRFError {
	return &SSRFError{
		URL:    url,
		Reason: reason,
		Cause:  cause,
	}
}

// Error implements the error interface
func (e *DownloadError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("Failed to download %s: %d %s", e.URL, e.StatusCode, e.StatusText)
	}
	if e.Cause != nil {
		return fmt.Sprintf("Failed to download %s: %v", e.URL, e.Cause)
	}
	return fmt.Sprintf("Failed to download %s", e.URL)
}

// Unwrap returns the underlying cause
func (e *DownloadError) Unwrap() error {
	return e.Cause
}

// IsDownloadError checks if an error is a DownloadError
func IsDownloadError(err error) bool {
	var downloadErr *DownloadError
	return errors.As(err, &downloadErr)
}

// NewDownloadError creates a new download error
func NewDownloadError(url string, statusCode int, statusText, message string, cause error) *DownloadError {
	return &DownloadError{
		URL:        url,
		StatusCode: statusCode,
		StatusText: statusText,
		Message:    message,
		Cause:      cause,
	}
}
