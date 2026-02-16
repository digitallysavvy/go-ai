package types

import "fmt"

// MissingToolResultError indicates that a provider did not return an expected tool result
// This is used specifically for provider-executed (deferrable) tools where the provider
// should have returned a result but didn't
type MissingToolResultError struct {
	// ToolCallID is the ID of the tool call that's missing a result
	ToolCallID string

	// ToolName is the name of the tool that was called
	ToolName string

	// Message provides additional context about the missing result
	Message string
}

// Error implements the error interface
func (e *MissingToolResultError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("missing tool result for call %s (tool: %s): %s", e.ToolCallID, e.ToolName, e.Message)
	}
	return fmt.Sprintf("missing tool result for call %s (tool: %s)", e.ToolCallID, e.ToolName)
}

// MissingToolResultsError represents multiple missing tool results
// This can occur when multiple provider-executed tools fail to return results
type MissingToolResultsError struct {
	// ToolCallIDs is a list of tool call IDs that are missing results
	ToolCallIDs []string

	// Message provides additional context
	Message string
}

// Error implements the error interface
func (e *MissingToolResultsError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("missing tool results for %d call(s) %v: %s", len(e.ToolCallIDs), e.ToolCallIDs, e.Message)
	}
	return fmt.Sprintf("missing tool results for %d call(s): %v", len(e.ToolCallIDs), e.ToolCallIDs)
}

// ToolExecutionError wraps errors that occur during tool execution
// This provides additional context about which tool failed and why
type ToolExecutionError struct {
	// ToolCallID is the ID of the tool call that failed
	ToolCallID string

	// ToolName is the name of the tool that failed
	ToolName string

	// Err is the underlying error
	Err error

	// ProviderExecuted indicates if this was a provider-executed tool
	ProviderExecuted bool
}

// Error implements the error interface
func (e *ToolExecutionError) Error() string {
	executionType := "local"
	if e.ProviderExecuted {
		executionType = "provider-executed"
	}
	return fmt.Sprintf("tool execution failed [%s] (tool: %s, call: %s): %v", executionType, e.ToolName, e.ToolCallID, e.Err)
}

// Unwrap allows errors.Unwrap to access the underlying error
func (e *ToolExecutionError) Unwrap() error {
	return e.Err
}
