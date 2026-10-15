package mcp

import "fmt"

// MCPClientError represents an error from the MCP client.
//
// Code is the JSON-RPC application error code from an MCP error payload.
// StatusCode is the HTTP transport status for streamable HTTP failures. These
// fields are intentionally distinct so callers can branch on protocol errors
// and transport failures independently.
type MCPClientError struct {
	Code         int
	Message      string
	Data         interface{}
	StatusCode   int
	URL          string
	ResponseBody string

	// Cause is the underlying error (TS `cause`), for example the context
	// error behind "Request was aborted". It is reachable with errors.Is and
	// errors.As.
	Cause error
}

// Error returns the bare message, like the TS MCPClientError: for a JSON-RPC
// error that is the server's own error text, without a code prefix. Use Code
// and Data for the structured fields.
func (e *MCPClientError) Error() string {
	return e.Message
}

// Unwrap returns Cause.
func (e *MCPClientError) Unwrap() error {
	return e.Cause
}

// MCPClientErrorOption configures optional fields on MCPClientError.
type MCPClientErrorOption func(*MCPClientError)

// WithMCPHTTPStatus records the HTTP status code for an MCP transport error.
func WithMCPHTTPStatus(statusCode int) MCPClientErrorOption {
	return func(e *MCPClientError) {
		e.StatusCode = statusCode
	}
}

// WithMCPHTTPURL records the endpoint URL for an MCP transport error.
func WithMCPHTTPURL(url string) MCPClientErrorOption {
	return func(e *MCPClientError) {
		e.URL = url
	}
}

// WithMCPHTTPResponseBody records the response body for an MCP transport error.
func WithMCPHTTPResponseBody(body string) MCPClientErrorOption {
	return func(e *MCPClientError) {
		e.ResponseBody = body
	}
}

// WithMCPHTTPResponse records the structured HTTP fields for an MCP transport error.
func WithMCPHTTPResponse(statusCode int, url string, responseBody string) MCPClientErrorOption {
	return func(e *MCPClientError) {
		e.StatusCode = statusCode
		e.URL = url
		e.ResponseBody = responseBody
	}
}

// NewMCPClientError creates a new MCP client error
func NewMCPClientError(code int, message string, data interface{}, opts ...MCPClientErrorOption) *MCPClientError {
	err := &MCPClientError{
		Code:    code,
		Message: message,
		Data:    data,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(err)
		}
	}
	return err
}

// Common MCP errors
var (
	ErrParseError     = &MCPClientError{Code: ErrorCodeParseError, Message: "Parse error"}
	ErrInvalidRequest = &MCPClientError{Code: ErrorCodeInvalidRequest, Message: "Invalid request"}
	ErrMethodNotFound = &MCPClientError{Code: ErrorCodeMethodNotFound, Message: "Method not found"}
	ErrInvalidParams  = &MCPClientError{Code: ErrorCodeInvalidParams, Message: "Invalid params"}
	ErrInternalError  = &MCPClientError{Code: ErrorCodeInternalError, Message: "Internal error"}

	ErrToolNotFound      = &MCPClientError{Code: ErrorCodeToolNotFound, Message: "Tool not found"}
	ErrToolExecutionFail = &MCPClientError{Code: ErrorCodeToolExecutionFail, Message: "Tool execution failed"}
	ErrResourceNotFound  = &MCPClientError{Code: ErrorCodeResourceNotFound, Message: "Resource not found"}
	ErrUnauthorized      = &MCPClientError{Code: ErrorCodeUnauthorized, Message: "Unauthorized"}
)

// TransportError represents a transport-level error
type TransportError struct {
	Message string
	Cause   error
}

func (e *TransportError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("transport error: %s: %v", e.Message, e.Cause)
	}
	return fmt.Sprintf("transport error: %s", e.Message)
}

func (e *TransportError) Unwrap() error {
	return e.Cause
}

// NewTransportError creates a new transport error
func NewTransportError(message string, cause error) *TransportError {
	return &TransportError{
		Message: message,
		Cause:   cause,
	}
}

// TimeoutError represents a timeout error
type TimeoutError struct {
	Operation string
	// TimeoutMS is the elapsed limit. When set, the message is the TS one,
	// "Request timed out after <n>ms".
	TimeoutMS int
}

func (e *TimeoutError) Error() string {
	if e.TimeoutMS > 0 {
		return fmt.Sprintf("Request timed out after %dms", e.TimeoutMS)
	}
	return fmt.Sprintf("timeout: %s", e.Operation)
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(operation string) *TimeoutError {
	return &TimeoutError{
		Operation: operation,
	}
}
