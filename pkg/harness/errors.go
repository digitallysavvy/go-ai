package harness

import (
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
)

// Error names, matching the TS `name` property of each error class.
const (
	HarnessErrorName                     = "AI_HarnessError"
	CapabilityUnsupportedErrorName       = "AI_HarnessCapabilityUnsupportedError"
	SandboxAuthenticationErrorName       = "AI_HarnessSandboxAuthenticationError"
	HistoryUnavailableErrorName          = "AI_HarnessHistoryUnavailableError"
	defaultHarnessClientSafeErrorMessage = "An error occurred."
)

// harnessErr is implemented by *HarnessError and every error type that embeds
// it (TS: `HarnessError.isInstance` matches subclasses via the marker).
type harnessErr interface {
	error
	harnessError() *HarnessError
}

// HarnessError is the base error for failures originating in or signalled by a
// harness adapter. Mirrors TS `HarnessError` (`AI_HarnessError`).
//
// Specialized errors embed HarnessError. A custom error type may embed it too
// and set Name to its own value; such errors are treated as unreviewed by
// GetHarnessErrorMessage.
type HarnessError struct {
	// Name defaults to "AI_HarnessError".
	Name    string
	Message string
	Cause   error
}

// NewHarnessError returns a base harness error.
func NewHarnessError(message string, cause error) *HarnessError {
	return &HarnessError{Name: HarnessErrorName, Message: message, Cause: cause}
}

func (e *HarnessError) Error() string { return e.Message }

// Unwrap returns the cause.
func (e *HarnessError) Unwrap() error { return e.Cause }

// ErrorName returns the TS-compatible error name.
func (e *HarnessError) ErrorName() string {
	if e.Name == "" {
		return HarnessErrorName
	}
	return e.Name
}

func (e *HarnessError) harnessError() *HarnessError { return e }

// CapabilityUnsupportedError is returned when a caller asks the harness to do
// something the adapter (or the supplied sandbox) does not support. Mirrors TS
// `HarnessCapabilityUnsupportedError`.
type CapabilityUnsupportedError struct {
	HarnessError
	// HarnessID is optional structured context.
	HarnessID string
}

// NewCapabilityUnsupportedError builds a CapabilityUnsupportedError.
func NewCapabilityUnsupportedError(message, harnessID string, cause error) *CapabilityUnsupportedError {
	return &CapabilityUnsupportedError{
		HarnessError: HarnessError{Name: CapabilityUnsupportedErrorName, Message: message, Cause: cause},
		HarnessID:    harnessID,
	}
}

// SandboxAuthenticationError is returned when a sandbox provider cannot
// authenticate or authorize the operation needed to create or resume a harness
// sandbox. Mirrors TS `HarnessSandboxAuthenticationError`.
type SandboxAuthenticationError struct {
	HarnessError
	SandboxProviderID string
}

// NewSandboxAuthenticationError builds a SandboxAuthenticationError.
func NewSandboxAuthenticationError(message, sandboxProviderID string, cause error) *SandboxAuthenticationError {
	return &SandboxAuthenticationError{
		HarnessError:      HarnessError{Name: SandboxAuthenticationErrorName, Message: message, Cause: cause},
		SandboxProviderID: sandboxProviderID,
	}
}

// HistoryUnavailableError is thrown by AgentSession.ReadHistory when the
// adapter supports history reads but cannot reach the runtime's store from
// the current environment — for example the store lives inside a remote
// sandbox, or the transcript directory is missing or unreadable.
//
// Distinct from CapabilityUnsupportedError (the adapter does not implement
// history reads at all) so hosts can retry or degrade differently. A
// conversation with no recorded messages yet is not an error; it resolves to
// an empty result instead. Mirrors TS `HarnessHistoryUnavailableError`.
type HistoryUnavailableError struct {
	HarnessError
	// HarnessID is optional structured context.
	HarnessID string
}

// NewHistoryUnavailableError builds a HistoryUnavailableError.
func NewHistoryUnavailableError(message, harnessID string, cause error) *HistoryUnavailableError {
	return &HistoryUnavailableError{
		HarnessError: HarnessError{Name: HistoryUnavailableErrorName, Message: message, Cause: cause},
		HarnessID:    harnessID,
	}
}

// IsHistoryUnavailableError mirrors `HarnessHistoryUnavailableError.isInstance`.
func IsHistoryUnavailableError(err error) bool {
	var e *HistoryUnavailableError
	return errors.As(err, &e)
}

// IsHarnessError reports whether err is (or wraps) a HarnessError or any error
// embedding it. Mirrors `HarnessError.isInstance`.
func IsHarnessError(err error) bool {
	var h harnessErr
	return errors.As(err, &h)
}

// IsCapabilityUnsupportedError mirrors `HarnessCapabilityUnsupportedError.isInstance`.
func IsCapabilityUnsupportedError(err error) bool {
	var e *CapabilityUnsupportedError
	return errors.As(err, &e)
}

// IsSandboxAuthenticationError mirrors `HarnessSandboxAuthenticationError.isInstance`.
func IsSandboxAuthenticationError(err error) bool {
	var e *SandboxAuthenticationError
	return errors.As(err, &e)
}

// GetHarnessErrorMessage returns a client-safe message for errors produced by
// the harness runtime. Messages from reviewed harness error types are
// preserved; all other errors are masked. Mirrors TS `getHarnessErrorMessage`.
func GetHarnessErrorMessage(err error) string {
	// TS inspects the thrown value itself (isInstance is a marker check on that
	// value, never on its cause). The nearest harness error in the chain is the
	// closest Go analogue of "the value", so an unreviewed or base harness
	// error that wraps a reviewed error as its cause is judged by its own
	// type and message.
	for e := err; e != nil; e = errors.Unwrap(e) {
		h, ok := e.(harnessErr)
		if !ok {
			continue
		}
		switch typed := h.(type) {
		case *CapabilityUnsupportedError:
			return typed.Message
		case *SandboxAuthenticationError:
			return typed.Message
		}
		base := h.harnessError()
		if base.ErrorName() == HarnessErrorName {
			return base.Message
		}
		return defaultHarnessClientSafeErrorMessage
	}
	return defaultHarnessClientSafeErrorMessage
}

// ErrorValueMessage renders an arbitrary rejected value the way TS
// `getErrorMessage` does: nil is "unknown error", a string is itself, an error
// is its message, and anything else (a decoded wire error object) is its
// JSON.stringify text, not Go's `%v` map formatting.
func ErrorValueMessage(v any) string {
	switch e := v.(type) {
	case nil:
		return "unknown error"
	case string:
		return e
	case error:
		return e.Error()
	}
	b, err := jsstr.Marshal(v)
	if err != nil {
		return "unknown error"
	}
	return string(b)
}

type rawValueError struct {
	message string
	// value is the wire value the error was built from (TS keeps the raw
	// value, not an Error); hasValue is false for errors built from text.
	value    any
	hasValue bool
}

func (e *rawValueError) Error() string { return e.message }

// errorRawValue returns the wire value an ErrorFromValue error was built
// from.
func errorRawValue(err error) (any, bool) {
	var rv *rawValueError
	if errors.As(err, &rv) && rv.hasValue {
		return rv.value, true
	}
	return nil, false
}

// ErrorFromValue converts a wire error value to a Go error: an error passes
// through unchanged (so errors.As keeps working) and anything else becomes an
// error whose text is ErrorValueMessage(v).
func ErrorFromValue(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return &rawValueError{message: ErrorValueMessage(v), value: v, hasValue: true}
}
