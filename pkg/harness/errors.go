package harness

import "errors"

// Error names, matching the TS `name` property of each error class.
const (
	HarnessErrorName                     = "AI_HarnessError"
	CapabilityUnsupportedErrorName       = "AI_HarnessCapabilityUnsupportedError"
	SandboxAuthenticationErrorName       = "AI_HarnessSandboxAuthenticationError"
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
	var capability *CapabilityUnsupportedError
	if errors.As(err, &capability) {
		return capability.Message
	}
	var auth *SandboxAuthenticationError
	if errors.As(err, &auth) {
		return auth.Message
	}
	var h harnessErr
	if errors.As(err, &h) {
		base := h.harnessError()
		if base.ErrorName() == HarnessErrorName {
			return base.Message
		}
	}
	return defaultHarnessClientSafeErrorMessage
}
