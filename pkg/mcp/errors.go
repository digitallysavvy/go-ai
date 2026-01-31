package mcp

import "fmt"

// MCPClientError represents an error from the MCP client
type MCPClientError struct {
	Code    int
	Message string
	Data    interface{}
}

func (e *MCPClientError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("MCP error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

// NewMCPClientError creates a new MCP client error
func NewMCPClientError(code int, message string, data interface{}) *MCPClientError {
	return &MCPClientError{
		Code:    code,
		Message: message,
		Data:    data,
	}
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
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout: %s", e.Operation)
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(operation string) *TimeoutError {
	return &TimeoutError{
		Operation: operation,
	}
}
