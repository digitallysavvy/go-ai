package codemode

import "testing"

// Ports the error shape assertions from TypeScript's
// code-mode/src/errors.ts consumers (exceptions.test.ts and the error
// class doc comments): each error has a stable code and a readable
// message, and every constructor produces a value implementing
// CodeModeError.
func TestErrors_CodesAndMessages(t *testing.T) {
	cases := []struct {
		name    string
		err     CodeModeError
		code    string
		message string
	}{
		{"Timeout", NewTimeoutError(30000), "CODE_MODE_TIMEOUT", "Code mode execution timed out after 30000ms."},
		{"Aborted", NewAbortedError(), "CODE_MODE_ABORTED", "Code mode execution was aborted."},
		{"Concurrency", NewConcurrencyError(32), "CODE_MODE_CONCURRENCY_LIMIT", "Code mode maxWorkers limit reached (32)."},
		{"SourceTooLarge", NewSourceTooLargeError(300, 256), "CODE_MODE_SOURCE_TOO_LARGE", "Code mode source exceeds the 256 byte size limit."},
		{"BridgeLimit", NewBridgeLimitError("too many bridge requests", nil), "CODE_MODE_BRIDGE_LIMIT", "too many bridge requests"},
		{"DetachedBridgeRequest", NewDetachedBridgeRequestError("detached", nil), "CODE_MODE_DETACHED_BRIDGE_REQUEST", "detached"},
		{"Protocol", NewProtocolError("bad protocol", nil), "CODE_MODE_PROTOCOL_ERROR", "bad protocol"},
		{"Tool", NewToolError("tool failed", nil), "CODE_MODE_TOOL_ERROR", "tool failed"},
		{"ToolApprovalRequired", NewToolApprovalRequiredError("guarded", map[string]interface{}{}, "call-1"), "CODE_MODE_TOOL_APPROVAL_REQUIRED", `Tool "guarded" requires approval before execution.`},
		{"ToolApprovalDeniedNoReason", NewToolApprovalDeniedError("guarded", map[string]interface{}{}, "call-1", ""), "CODE_MODE_TOOL_APPROVAL_DENIED", `Tool "guarded" approval was denied.`},
		{"ToolApprovalDeniedWithReason", NewToolApprovalDeniedError("guarded", map[string]interface{}{}, "call-1", "no"), "CODE_MODE_TOOL_APPROVAL_DENIED", `Tool "guarded" approval was denied: no`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.ErrorCode(); got != tc.code {
				t.Errorf("code: got %q, want %q", got, tc.code)
			}
			if got := tc.err.Error(); got != tc.message {
				t.Errorf("message: got %q, want %q", got, tc.message)
			}
		})
	}
}

func TestErrors_BaseErrorDefaultsCode(t *testing.T) {
	err := NewError("oops", "", nil)
	if err.ErrorCode() != "CODE_MODE_ERROR" {
		t.Fatalf("got %q", err.ErrorCode())
	}
}
