package errors

import (
	"encoding/json"
	"errors"
	"strconv"
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
// provider gave.
//
// data, when a map[string]interface{} (the shape a provider gets by
// json.Unmarshal-ing a raw error payload into map[string]interface{} and
// attaching it as StreamChunk.Err's underlying data, or passing it directly
// here), is inspected the same way TS's normalizeStreamProviderError
// inspects the raw error object: type/code/statusCode (and their
// status_code/status/snake_case/isRetryable/is_retryable variants) are read
// from a nested `response.error` or `error` object when present, else from
// the top-level map. No provider currently attaches such structured data
// (see providerName/data doc on the pkg/ai/stream.go call site) — until one
// does, this falls back to wrapping err.Error() as the message, exactly as
// before. providerName is used verbatim; it can't be recovered from err
// itself.
func NormalizeStreamProviderError(err error, providerName string, data interface{}) error {
	if err == nil {
		return nil
	}
	if IsStreamProviderError(err) || IsProviderError(err) {
		return err
	}

	message := err.Error()
	var errType string
	var code interface{}
	var statusCode *int
	var explicitRetryable *bool

	if outer, ok := asRecord(data); ok {
		details := outer
		if nested, ok := asRecord(outer["response"]); ok {
			if errObj, ok := asRecord(nested["error"]); ok {
				details = errObj
			}
		} else if errObj, ok := asRecord(outer["error"]); ok {
			details = errObj
		}

		if m, ok := details["message"].(string); ok && m != "" {
			message = m
		}

		errType = firstString(details["type"], outer["type"])
		code = firstStringOrNumber(details["code"], outer["code"])
		statusCode = firstHTTPStatusCode(
			details["statusCode"], outer["statusCode"],
			details["status_code"], outer["status_code"],
			details["status"], outer["status"],
			details["code"], outer["code"],
		)
		explicitRetryable = firstBool(
			details["isRetryable"], outer["isRetryable"],
			details["is_retryable"], outer["is_retryable"],
		)
	}

	return NewStreamProviderError(message, providerName, errType, code, statusCode, explicitRetryable, data)
}

// asRecord mirrors TS normalize-stream-provider-error.ts's asRecord: it
// returns v as a map[string]interface{} when it is one.
func asRecord(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

func firstString(values ...interface{}) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstStringOrNumber(values ...interface{}) interface{} {
	for _, v := range values {
		switch v.(type) {
		case string, float64, int, int64, json.Number:
			return v
		}
	}
	return nil
}

func firstBool(values ...interface{}) *bool {
	for _, v := range values {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

// firstHTTPStatusCode returns the first value that parses as an HTTP status
// code (an integer, or a 3-digit numeric string, in [400, 599]), mirroring
// TS's getHttpStatusCode.
func firstHTTPStatusCode(values ...interface{}) *int {
	for _, v := range values {
		var n int
		switch t := v.(type) {
		case float64:
			n = int(t)
			if float64(n) != t {
				continue
			}
		case int:
			n = t
		case int64:
			n = int(t)
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				continue
			}
			n = int(f)
			if float64(n) != f {
				continue
			}
		case string:
			if len(t) != 3 {
				continue
			}
			parsed, err := strconv.Atoi(t)
			if err != nil {
				continue
			}
			n = parsed
		default:
			continue
		}
		if n >= 400 && n <= 599 {
			return &n
		}
	}
	return nil
}
