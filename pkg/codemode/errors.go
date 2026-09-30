package codemode

import "fmt"

// BaseError is the base type for errors raised by code mode. All
// package-specific errors carry a stable, machine-readable Code and may
// carry structured Details for diagnostics. It mirrors the TypeScript SDK's
// CodeModeError (code-mode/src/errors.ts).
type BaseError struct {
	// Message is the human-readable error message.
	Message string

	// Code is a stable machine-readable error code, e.g. "CODE_MODE_TIMEOUT".
	Code string

	// Details carries optional structured diagnostic details.
	Details interface{}
}

// NewError creates a base Error with the given message, code and details.
func NewError(message, code string, details interface{}) *BaseError {
	if code == "" {
		code = "CODE_MODE_ERROR"
	}
	return &BaseError{Message: message, Code: code, Details: details}
}

func (e *BaseError) Error() string { return e.Message }

// ErrorCode returns e.Code. It lets callers type-switch on the CodeModeError
// interface without depending on the concrete *BaseError type.
func (e *BaseError) ErrorCode() string { return e.Code }

// ErrorDetails returns e.Details.
func (e *BaseError) ErrorDetails() interface{} { return e.Details }

// CodeModeError is implemented by every error type in this package. It
// mirrors the TypeScript SDK's CodeModeError base class shape (a stable
// `code` plus optional `details`).
type CodeModeError interface {
	error
	ErrorCode() string
	ErrorDetails() interface{}
}

var (
	_ CodeModeError = (*BaseError)(nil)
	_ CodeModeError = (*TimeoutError)(nil)
	_ CodeModeError = (*AbortedError)(nil)
	_ CodeModeError = (*ConcurrencyError)(nil)
	_ CodeModeError = (*SourceTooLargeError)(nil)
	_ CodeModeError = (*BridgeLimitError)(nil)
	_ CodeModeError = (*DetachedBridgeRequestError)(nil)
	_ CodeModeError = (*ProtocolError)(nil)
	_ CodeModeError = (*ToolError)(nil)
	_ CodeModeError = (*ToolApprovalRequiredError)(nil)
	_ CodeModeError = (*ToolApprovalDeniedError)(nil)
)

// TimeoutError is raised when a sandbox invocation exceeds its timeout.
// Mirrors TypeScript's CodeModeTimeoutError.
type TimeoutError struct{ *BaseError }

// NewTimeoutError creates a TimeoutError for the given timeout budget.
func NewTimeoutError(timeoutMs int) *TimeoutError {
	return &TimeoutError{NewError(
		fmt.Sprintf("Code mode execution timed out after %dms.", timeoutMs),
		"CODE_MODE_TIMEOUT",
		map[string]interface{}{"timeoutMs": timeoutMs},
	)}
}

// AbortedError is raised when the caller's context is canceled during a
// code-mode invocation. Mirrors TypeScript's CodeModeAbortedError.
type AbortedError struct{ *BaseError }

// NewAbortedError creates an AbortedError.
func NewAbortedError() *AbortedError {
	return &AbortedError{NewError("Code mode execution was aborted.", "CODE_MODE_ABORTED", nil)}
}

// ConcurrencyError is raised when the process-global worker/concurrency cap
// has been reached. Mirrors TypeScript's CodeModeConcurrencyError
// (configured there via setMaxWorkers; see SetMaxWorkers in this package).
type ConcurrencyError struct{ *BaseError }

// NewConcurrencyError creates a ConcurrencyError.
func NewConcurrencyError(maxWorkers int) *ConcurrencyError {
	return &ConcurrencyError{NewError(
		fmt.Sprintf("Code mode maxWorkers limit reached (%d).", maxWorkers),
		"CODE_MODE_CONCURRENCY_LIMIT",
		map[string]interface{}{"maxWorkers": maxWorkers},
	)}
}

// SourceTooLargeError is raised when the provided source exceeds
// ExecutionPolicy.MaxSourceBytes. Mirrors TypeScript's
// CodeModeSourceTooLargeError.
type SourceTooLargeError struct{ *BaseError }

// NewSourceTooLargeError creates a SourceTooLargeError.
func NewSourceTooLargeError(bytes, maxBytes int) *SourceTooLargeError {
	return &SourceTooLargeError{NewError(
		fmt.Sprintf("Code mode source exceeds the %d byte size limit.", maxBytes),
		"CODE_MODE_SOURCE_TOO_LARGE",
		map[string]interface{}{"bytes": bytes, "maxBytes": maxBytes},
	)}
}

// BridgeLimitError is raised when sandboxed code exceeds bridge request
// limits (ExecutionPolicy.MaxBridgeRequests /
// MaxInFlightBridgeRequests). Mirrors TypeScript's
// CodeModeBridgeLimitError.
type BridgeLimitError struct{ *BaseError }

// NewBridgeLimitError creates a BridgeLimitError.
func NewBridgeLimitError(message string, details interface{}) *BridgeLimitError {
	return &BridgeLimitError{NewError(message, "CODE_MODE_BRIDGE_LIMIT", details)}
}

// DetachedBridgeRequestError is raised when sandboxed code starts host
// bridge work and returns without awaiting or otherwise observing it.
// Mirrors TypeScript's CodeModeDetachedBridgeRequestError (`run`'s own
// __runAssertNoDetachedBridgeCalls). A call that raises a code-mode
// interruption (RequestCodeModeInterrupt, or approval under
// ApprovalModeInterrupt) deliberately leaves its Promise unresolved (see
// the package doc's "Host tool bridge dispatch" section) so the sandboxed
// script can keep making synchronous progress; if the script never
// `await`s (or otherwise observes) that Promise at all -- a fire-and-forget
// `tools.x(input);` -- and completes anyway, that interruption was started
// but never surfaced to the caller for resolution, and RunCodeMode raises
// this error instead of silently returning a result that discards it.
type DetachedBridgeRequestError struct{ *BaseError }

// NewDetachedBridgeRequestError creates a DetachedBridgeRequestError.
func NewDetachedBridgeRequestError(message string, details interface{}) *DetachedBridgeRequestError {
	return &DetachedBridgeRequestError{NewError(message, "CODE_MODE_DETACHED_BRIDGE_REQUEST", details)}
}

// ProtocolError is raised when the host/sandbox protocol observes an
// invalid or mismatched message. Mirrors TypeScript's
// CodeModeProtocolError.
type ProtocolError struct{ *BaseError }

// NewProtocolError creates a ProtocolError.
func NewProtocolError(message string, details interface{}) *ProtocolError {
	return &ProtocolError{NewError(message, "CODE_MODE_PROTOCOL_ERROR", details)}
}

// ToolError is the base type for failures caused by nested host tool
// execution. Mirrors TypeScript's CodeModeToolError.
type ToolError struct{ *BaseError }

// NewToolError creates a ToolError.
func NewToolError(message string, details interface{}) *ToolError {
	return &ToolError{NewError(message, "CODE_MODE_TOOL_ERROR", details)}
}

// ToolApprovalRequiredError is raised when a host tool requires approval but
// no approval decision was obtained (approval mode "callback" with no
// OnApprovalRequired callback configured, or the callback returned no
// decision). Mirrors TypeScript's CodeModeToolApprovalRequiredError.
type ToolApprovalRequiredError struct{ *BaseError }

// NewToolApprovalRequiredError creates a ToolApprovalRequiredError.
func NewToolApprovalRequiredError(toolName string, input interface{}, toolCallID string) *ToolApprovalRequiredError {
	return &ToolApprovalRequiredError{NewError(
		fmt.Sprintf("Tool %q requires approval before execution.", toolName),
		"CODE_MODE_TOOL_APPROVAL_REQUIRED",
		map[string]interface{}{"toolName": toolName, "input": input, "toolCallId": toolCallID},
	)}
}

// ToolApprovalDeniedError is raised when a host tool's approval was denied.
// Mirrors TypeScript's CodeModeToolApprovalDeniedError.
type ToolApprovalDeniedError struct{ *BaseError }

// NewToolApprovalDeniedError creates a ToolApprovalDeniedError.
func NewToolApprovalDeniedError(toolName string, input interface{}, toolCallID, reason string) *ToolApprovalDeniedError {
	msg := fmt.Sprintf("Tool %q approval was denied.", toolName)
	details := map[string]interface{}{"toolName": toolName, "input": input, "toolCallId": toolCallID}
	if reason != "" {
		msg = fmt.Sprintf("Tool %q approval was denied: %s", toolName, reason)
		details["reason"] = reason
	}
	return &ToolApprovalDeniedError{NewError(msg, "CODE_MODE_TOOL_APPROVAL_DENIED", details)}
}
