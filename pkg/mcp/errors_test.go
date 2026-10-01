package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestMCPClientErrorFormattingAndConstructor(t *testing.T) {
	err := NewMCPClientError(400, "bad request", nil)
	if err.Code != 400 || err.Message != "bad request" || err.Data != nil {
		t.Fatalf("NewMCPClientError mismatch: %#v", err)
	}
	if got := err.Error(); got != "MCP error 400: bad request" {
		t.Fatalf("Error() = %q", got)
	}

	withData := NewMCPClientError(500, "boom", map[string]interface{}{"id": 1})
	if got := withData.Error(); !strings.Contains(got, "MCP error 500: boom") || !strings.Contains(got, "data: map[id:1]") {
		t.Fatalf("Error() with data = %q", got)
	}
}

func TestMCPClientErrorStructuredHTTPFieldsAndErrorsAs(t *testing.T) {
	err := NewMCPClientError(0, "POSTing to endpoint", nil,
		WithMCPHTTPResponse(503, "http://localhost:4000/mcp", "Service Unavailable"),
	)
	wrapped := NewTransportError("failed to send request", err)

	var clientErr *MCPClientError
	if !errors.As(wrapped, &clientErr) {
		t.Fatalf("errors.As(%T) failed", wrapped)
	}
	if clientErr.Code != 0 {
		t.Fatalf("Code = %d, want JSON-RPC zero value for HTTP-only error", clientErr.Code)
	}
	if clientErr.StatusCode != 503 || clientErr.URL != "http://localhost:4000/mcp" || clientErr.ResponseBody != "Service Unavailable" {
		t.Fatalf("structured HTTP fields mismatch: %#v", clientErr)
	}
}

func TestTransportErrorFormattingAndUnwrap(t *testing.T) {
	cause := errors.New("dial timeout")
	err := NewTransportError("send failed", cause)
	if err.Message != "send failed" || err.Cause != cause {
		t.Fatalf("NewTransportError mismatch: %#v", err)
	}
	if !strings.Contains(err.Error(), "transport error: send failed: dial timeout") {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to match wrapped cause")
	}
	if err.Unwrap() != cause {
		t.Fatal("Unwrap() should return cause")
	}

	withoutCause := &TransportError{Message: "not connected"}
	if got := withoutCause.Error(); got != "transport error: not connected" {
		t.Fatalf("Error() without cause = %q", got)
	}
}

func TestTimeoutErrorFormattingAndConstructor(t *testing.T) {
	err := NewTimeoutError("receive response")
	if err.Operation != "receive response" {
		t.Fatalf("operation = %q", err.Operation)
	}
	if got := err.Error(); got != "timeout: receive response" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestWellKnownMCPErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     *MCPClientError
		code    int
		message string
	}{
		{name: "parse", err: ErrParseError, code: ErrorCodeParseError, message: "Parse error"},
		{name: "invalid request", err: ErrInvalidRequest, code: ErrorCodeInvalidRequest, message: "Invalid request"},
		{name: "method not found", err: ErrMethodNotFound, code: ErrorCodeMethodNotFound, message: "Method not found"},
		{name: "invalid params", err: ErrInvalidParams, code: ErrorCodeInvalidParams, message: "Invalid params"},
		{name: "internal", err: ErrInternalError, code: ErrorCodeInternalError, message: "Internal error"},
		{name: "tool not found", err: ErrToolNotFound, code: ErrorCodeToolNotFound, message: "Tool not found"},
		{name: "tool execution", err: ErrToolExecutionFail, code: ErrorCodeToolExecutionFail, message: "Tool execution failed"},
		{name: "resource not found", err: ErrResourceNotFound, code: ErrorCodeResourceNotFound, message: "Resource not found"},
		{name: "unauthorized", err: ErrUnauthorized, code: ErrorCodeUnauthorized, message: "Unauthorized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code || tt.err.Message != tt.message {
				t.Fatalf("error mismatch: got (%d,%q), want (%d,%q)", tt.err.Code, tt.err.Message, tt.code, tt.message)
			}
		})
	}
}
