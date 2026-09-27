package errors

import (
	"errors"
	"strings"
)

// StreamProviderError is a normalized error reported by a provider after a
// model response stream has already started (a mid-stream "error" chunk),
// mirroring TS error/stream-provider-error.ts (audit row 35841f5 / WG8).
// Unlike ProviderError (which represents a failure to start the call at
// all), a StreamProviderError always means some output may already have
// been produced before the provider gave up.
type StreamProviderError struct {
	// Provider name, when known.
	Provider string

	// Type is the provider-defined error type, when supplied.
	Type string

	// Code is the provider-defined error code, when supplied. Either a
	// string or a number (json.Number/float64/int), mirroring the provider's
	// own representation.
	Code interface{}

	// StatusCode is the HTTP-equivalent status code, when supplied by or
	// inferable from the provider error payload.
	StatusCode *int

	// IsRetryable reports whether retrying the model call may succeed.
	IsRetryable bool

	// Data is the original provider error payload.
	Data interface{}

	// Message is the human-readable error message.
	Message string
}

func (e *StreamProviderError) Error() string {
	if e == nil || e.Message == "" {
		return "stream provider error"
	}
	return e.Message
}

// IsStreamProviderError reports whether err is a *StreamProviderError.
func IsStreamProviderError(err error) bool {
	var target *StreamProviderError
	return errors.As(err, &target)
}

// isRetryableStatusCode mirrors TS stream-provider-error.ts's
// isRetryableStatusCode: 408/409/429 or any 5xx.
func isRetryableStatusCode(statusCode *int) bool {
	if statusCode == nil {
		return false
	}
	s := *statusCode
	return s == 408 || s == 409 || s == 429 || s >= 500
}

// inferredMessageMetadata is the (statusCode, isRetryable) pair TS infers
// from a small set of exact, case-insensitive provider error messages that
// carry no other structured metadata (normalize-stream-provider-error.ts
// inferExactMessageMetadata).
func inferredMessageMetadata(message string) (statusCode int, isRetryable bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "overloaded", "overloaded error", "model overloaded":
		return 503, true, true
	case "internal server error":
		return 500, true, true
	case "service unavailable":
		return 503, true, true
	default:
		return 0, false, false
	}
}

// NewStreamProviderError builds a StreamProviderError, applying the same
// isRetryable default TS uses when not explicitly overridden: the message
// metadata inference, else the status-code default.
func NewStreamProviderError(message, providerName, errType string, code interface{}, statusCode *int, isRetryable *bool, data interface{}) *StreamProviderError {
	resolvedStatus := statusCode
	resolvedRetryable := false
	if metaStatus, metaRetryable, ok := inferredMessageMetadata(message); ok {
		if resolvedStatus == nil {
			s := metaStatus
			resolvedStatus = &s
		}
		resolvedRetryable = metaRetryable
	} else {
		resolvedRetryable = isRetryableStatusCode(resolvedStatus)
	}
	if isRetryable != nil {
		resolvedRetryable = *isRetryable
	}
	return &StreamProviderError{
		Provider:    providerName,
		Type:        errType,
		Code:        code,
		StatusCode:  resolvedStatus,
		IsRetryable: resolvedRetryable,
		Data:        data,
		Message:     message,
	}
}

// NormalizeStreamProviderError normalizes a mid-stream provider error into a
// *StreamProviderError, mirroring TS prompt/normalize-stream-provider-error.ts.
// It leaves an error unchanged when it is already a well-known typed error
// (a *ProviderError, an existing *StreamProviderError, or any other error
// this SDK constructs deliberately) — normalization only applies to a raw,
// provider-shaped payload arriving as a plain error whose message is all a
// provider gave. providerName/data are used verbatim when they can't be
// recovered from err itself, matching the core normalization call site
// (pkg/ai/stream.go), which only has the chunk's Text message and,
// optionally, a structured Err/Raw value a provider attached.
func NormalizeStreamProviderError(err error, providerName string, data interface{}) error {
	if err == nil {
		return nil
	}
	if IsStreamProviderError(err) || IsProviderError(err) {
		return err
	}
	return NewStreamProviderError(err.Error(), providerName, "", nil, nil, nil, data)
}
