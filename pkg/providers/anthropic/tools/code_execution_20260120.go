package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AnthropicCodeExecutionToolType is the type identifier sent to the Anthropic API
// in the tools array when using the code execution tool (version 2026-01-20).
const AnthropicCodeExecutionToolType = "code-execution_20260120"

// BetaHeaderCodeExecution is the beta header value required for code execution (20260120).
// It is automatically injected by the Anthropic provider when this tool is in the tool list.
const BetaHeaderCodeExecution = "code-execution-20260120"

// codeExecution20260120ToolName is the Go-AI SDK internal name for this tool.
// The Anthropic provider detects this name to inject the required beta header
// and to format the API request correctly.
const codeExecution20260120ToolName = "anthropic.code_execution_20260120"

// Input type discriminator values.
const (
	CodeExecutionInputTypeProgrammatic = "programmatic-tool-call"
	CodeExecutionInputTypeBash         = "bash_code_execution"
	CodeExecutionInputTypeTextEditor   = "text_editor_code_execution"
)

// Result type discriminator values.
const (
	CodeExecutionResultTypeProgrammatic    = "code_execution_result"
	CodeExecutionResultTypeBash            = "bash_code_execution_result"
	CodeExecutionResultTypeBashError       = "bash_code_execution_tool_result_error"
	CodeExecutionResultTypeTextEditorError = "text_editor_code_execution_tool_result_error"
	CodeExecutionResultTypeViewResult      = "text_editor_code_execution_view_result"
	CodeExecutionResultTypeCreateResult    = "text_editor_code_execution_create_result"
	CodeExecutionResultTypeStrReplaceResult = "text_editor_code_execution_str_replace_result"
)

// CodeExecutionInput is the discriminated union interface for all code execution
// input types. Use UnmarshalCodeExecutionInput to decode from JSON.
type CodeExecutionInput interface {
	codeExecutionInput()
	// GetInputType returns the discriminator type string.
	GetInputType() string
}

// ProgrammaticToolCallInput represents a Python programmatic tool call.
// Sent by the model when it wants to execute Python code that may trigger
// client-executed tools via allowedCallers.
//
// type: "programmatic-tool-call"
type ProgrammaticToolCallInput struct {
	Type string `json:"type"` // "programmatic-tool-call"
	Code string `json:"code"` // Python code to execute
}

func (p *ProgrammaticToolCallInput) codeExecutionInput()    {}
func (p *ProgrammaticToolCallInput) GetInputType() string   { return p.Type }

// BashCodeExecutionInput represents a bash command execution request.
// Sent by the model when it wants to run a shell command.
//
// type: "bash_code_execution"
type BashCodeExecutionInput struct {
	Type    string `json:"type"`    // "bash_code_execution"
	Command string `json:"command"` // Shell command to execute
}

func (b *BashCodeExecutionInput) codeExecutionInput()    {}
func (b *BashCodeExecutionInput) GetInputType() string   { return b.Type }

// TextEditorInput represents a text editor file operation.
// The Command field specifies the sub-operation: "view", "create", or "str_replace".
//
// type: "text_editor_code_execution"
type TextEditorInput struct {
	Type    string `json:"type"`    // "text_editor_code_execution"
	Command string `json:"command"` // "view" | "create" | "str_replace"
	Path    string `json:"path"`

	// FileText is the file content for the "create" command (optional).
	FileText *string `json:"file_text,omitempty"`

	// OldStr is the string to find and replace (required for "str_replace").
	OldStr *string `json:"old_str,omitempty"`

	// NewStr is the replacement string (required for "str_replace").
	NewStr *string `json:"new_str,omitempty"`
}

func (t *TextEditorInput) codeExecutionInput()    {}
func (t *TextEditorInput) GetInputType() string   { return t.Type }

// UnmarshalCodeExecutionInput decodes a JSON-encoded CodeExecutionInput by reading
// the "type" discriminator field and returning the appropriate concrete type.
func UnmarshalCodeExecutionInput(data []byte) (CodeExecutionInput, error) {
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return nil, fmt.Errorf("code execution input: reading type field: %w", err)
	}

	switch disc.Type {
	case CodeExecutionInputTypeProgrammatic:
		var v ProgrammaticToolCallInput
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution input: unmarshal programmatic-tool-call: %w", err)
		}
		return &v, nil

	case CodeExecutionInputTypeBash:
		var v BashCodeExecutionInput
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution input: unmarshal bash_code_execution: %w", err)
		}
		return &v, nil

	case CodeExecutionInputTypeTextEditor:
		var v TextEditorInput
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution input: unmarshal text_editor_code_execution: %w", err)
		}
		return &v, nil

	default:
		return nil, fmt.Errorf("code execution input: unknown type %q", disc.Type)
	}
}

// CodeExecutionResult is the discriminated union interface for all code execution
// result types. Use UnmarshalCodeExecutionResult to decode from JSON.
type CodeExecutionResult interface {
	codeExecutionResult()
	// GetResultType returns the discriminator type string.
	GetResultType() string
}

// CodeExecutionOutputItem is an output file reference in a programmatic execution result.
type CodeExecutionOutputItem struct {
	Type   string `json:"type"`    // "code_execution_output"
	FileID string `json:"file_id"`
}

// BashCodeExecutionOutputItem is an output file reference in a bash execution result.
type BashCodeExecutionOutputItem struct {
	Type   string `json:"type"`    // "bash_code_execution_output"
	FileID string `json:"file_id"`
}

// ProgrammaticExecutionResult is returned after Python programmatic code execution.
//
// type: "code_execution_result"
type ProgrammaticExecutionResult struct {
	Type       string                    `json:"type"` // "code_execution_result"
	Stdout     string                    `json:"stdout"`
	Stderr     string                    `json:"stderr"`
	ReturnCode int                       `json:"return_code"`
	Content    []CodeExecutionOutputItem `json:"content,omitempty"`
}

func (r *ProgrammaticExecutionResult) codeExecutionResult()    {}
func (r *ProgrammaticExecutionResult) GetResultType() string   { return r.Type }

// BashExecutionResult is returned after a bash command execution.
//
// type: "bash_code_execution_result"
type BashExecutionResult struct {
	Type       string                       `json:"type"` // "bash_code_execution_result"
	Content    []BashCodeExecutionOutputItem `json:"content,omitempty"`
	Stdout     string                       `json:"stdout"`
	Stderr     string                       `json:"stderr"`
	ReturnCode int                          `json:"return_code"`
}

func (r *BashExecutionResult) codeExecutionResult()    {}
func (r *BashExecutionResult) GetResultType() string   { return r.Type }

// BashExecutionError is returned when bash execution fails with an error code.
// Available error codes: invalid_tool_input, unavailable, too_many_requests,
// execution_time_exceeded, output_file_too_large.
//
// type: "bash_code_execution_tool_result_error"
type BashExecutionError struct {
	Type      string `json:"type"`       // "bash_code_execution_tool_result_error"
	ErrorCode string `json:"error_code"`
}

func (r *BashExecutionError) codeExecutionResult()    {}
func (r *BashExecutionError) GetResultType() string   { return r.Type }

// TextEditorExecutionError is returned when a text editor operation fails.
// Available error codes: invalid_tool_input, unavailable, too_many_requests,
// execution_time_exceeded, file_not_found.
//
// type: "text_editor_code_execution_tool_result_error"
type TextEditorExecutionError struct {
	Type      string `json:"type"`       // "text_editor_code_execution_tool_result_error"
	ErrorCode string `json:"error_code"`
}

func (r *TextEditorExecutionError) codeExecutionResult()    {}
func (r *TextEditorExecutionError) GetResultType() string   { return r.Type }

// TextEditorViewResult is returned after a file view operation.
// FileType is one of: "text", "image", "pdf".
//
// type: "text_editor_code_execution_view_result"
type TextEditorViewResult struct {
	Type       string `json:"type"`        // "text_editor_code_execution_view_result"
	Content    string `json:"content"`
	FileType   string `json:"file_type"`
	NumLines   *int   `json:"num_lines"`
	StartLine  *int   `json:"start_line"`
	TotalLines *int   `json:"total_lines"`
}

func (r *TextEditorViewResult) codeExecutionResult()    {}
func (r *TextEditorViewResult) GetResultType() string   { return r.Type }

// TextEditorCreateResult is returned after a file create or update operation.
// IsFileUpdate is true when updating an existing file, false when creating a new one.
//
// type: "text_editor_code_execution_create_result"
type TextEditorCreateResult struct {
	Type         string `json:"type"`           // "text_editor_code_execution_create_result"
	IsFileUpdate bool   `json:"is_file_update"`
}

func (r *TextEditorCreateResult) codeExecutionResult()    {}
func (r *TextEditorCreateResult) GetResultType() string   { return r.Type }

// TextEditorStrReplaceResult is returned after a str_replace file operation.
//
// type: "text_editor_code_execution_str_replace_result"
type TextEditorStrReplaceResult struct {
	Type     string   `json:"type"`      // "text_editor_code_execution_str_replace_result"
	Lines    []string `json:"lines"`
	NewLines *int     `json:"new_lines"`
	NewStart *int     `json:"new_start"`
	OldLines *int     `json:"old_lines"`
	OldStart *int     `json:"old_start"`
}

func (r *TextEditorStrReplaceResult) codeExecutionResult()    {}
func (r *TextEditorStrReplaceResult) GetResultType() string   { return r.Type }

// UnmarshalCodeExecutionResult decodes a JSON-encoded CodeExecutionResult by reading
// the "type" discriminator field and returning the appropriate concrete type.
func UnmarshalCodeExecutionResult(data []byte) (CodeExecutionResult, error) {
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return nil, fmt.Errorf("code execution result: reading type field: %w", err)
	}

	switch disc.Type {
	case CodeExecutionResultTypeProgrammatic:
		var v ProgrammaticExecutionResult
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal code_execution_result: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeBash:
		var v BashExecutionResult
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal bash_code_execution_result: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeBashError:
		var v BashExecutionError
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal bash_code_execution_tool_result_error: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeTextEditorError:
		var v TextEditorExecutionError
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal text_editor_code_execution_tool_result_error: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeViewResult:
		var v TextEditorViewResult
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal text_editor_code_execution_view_result: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeCreateResult:
		var v TextEditorCreateResult
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal text_editor_code_execution_create_result: %w", err)
		}
		return &v, nil

	case CodeExecutionResultTypeStrReplaceResult:
		var v TextEditorStrReplaceResult
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("code execution result: unmarshal text_editor_code_execution_str_replace_result: %w", err)
		}
		return &v, nil

	default:
		return nil, fmt.Errorf("code execution result: unknown type %q", disc.Type)
	}
}

// CodeExecution20260120 creates the Anthropic code execution tool (version 2026-01-20).
//
// This tool enables the model to request client-side code execution. It supports
// three execution modes:
//   - Python programmatic tool calls (type: "programmatic-tool-call")
//   - Bash command execution (type: "bash_code_execution")
//   - Text editor file operations (type: "text_editor_code_execution")
//
// The beta header "code-execution-20260120" is automatically injected by the Anthropic
// provider when this tool is present in the tool list.
//
// Usage: when the model calls this tool, parse the input with UnmarshalCodeExecutionInput,
// execute the requested operation locally, then return a CodeExecutionResult as JSON.
//
// Supported models: Claude Opus 4.6, Claude Sonnet 4.6
//
// Example:
//
//	tool := anthropicTools.CodeExecution20260120()
func CodeExecution20260120() types.Tool {
	return types.Tool{
		Name:        codeExecution20260120ToolName,
		Description: buildCodeExecution20260120Description(),
		Parameters:  buildCodeExecution20260120Schema(),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("code execution tool requires client-side execution: parse input with UnmarshalCodeExecutionInput and return a CodeExecutionResult")
		},
	}
}

func buildCodeExecution20260120Description() string {
	return `Code execution tool for client-side Python, Bash, and file operations (version 2026-01-20).

When the model calls this tool, execute the requested operation locally and return the result.

Input Types:
1. Programmatic Tool Calling (type: "programmatic-tool-call")
   - Python code via 'code' parameter
   - May trigger client-executed tools via allowedCallers
   - Return: code_execution_result with stdout, stderr, return_code

2. Bash Code Execution (type: "bash_code_execution")
   - Shell commands via 'command' parameter
   - Return: bash_code_execution_result with stdout, stderr, return_code

3. Text Editor Code Execution (type: "text_editor_code_execution")
   - view: View file contents (requires 'path')
   - create: Create file with content (requires 'path', optional 'file_text')
   - str_replace: Replace strings in file (requires 'path', 'old_str', 'new_str')

Result Types:
- code_execution_result: Programmatic execution result
- bash_code_execution_result: Bash execution result
- text_editor_code_execution_view_result: File view result
- text_editor_code_execution_create_result: File creation/update result
- text_editor_code_execution_str_replace_result: String replacement result
- bash_code_execution_tool_result_error: Bash execution error
- text_editor_code_execution_tool_result_error: Text editor error

Use UnmarshalCodeExecutionInput to parse inputs and return a CodeExecutionResult as JSON.`
}

func buildCodeExecution20260120Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"oneOf": []interface{}{
			// 1. Programmatic tool calling
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": CodeExecutionInputTypeProgrammatic,
					},
					"code": map[string]interface{}{
						"type":        "string",
						"description": "Python code to execute when code_execution is used with allowedCallers to trigger client-executed tools.",
					},
				},
				"required": []string{"type", "code"},
			},
			// 2. Bash code execution
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": CodeExecutionInputTypeBash,
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute.",
					},
				},
				"required": []string{"type", "command"},
			},
			// 3. Text editor code execution (nested discriminated union for commands)
			buildCodeExecution20260120TextEditorSchema(),
		},
		"discriminator": map[string]interface{}{
			"propertyName": "type",
		},
	}
}

func buildCodeExecution20260120TextEditorSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"oneOf": []interface{}{
			// view command
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": CodeExecutionInputTypeTextEditor,
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       "view",
						"description": "View file contents.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to view.",
					},
				},
				"required": []string{"type", "command", "path"},
			},
			// create command
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": CodeExecutionInputTypeTextEditor,
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       "create",
						"description": "Create or update a file.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to create or update.",
					},
					"file_text": map[string]interface{}{
						"type":        "string",
						"description": "The text content of the file to create.",
					},
				},
				"required": []string{"type", "command", "path"},
			},
			// str_replace command
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": CodeExecutionInputTypeTextEditor,
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       "str_replace",
						"description": "Replace a string in a file.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to edit.",
					},
					"old_str": map[string]interface{}{
						"type":        "string",
						"description": "The string to replace.",
					},
					"new_str": map[string]interface{}{
						"type":        "string",
						"description": "The new string to replace the old string with.",
					},
				},
				"required": []string{"type", "command", "path", "old_str", "new_str"},
			},
		},
		"discriminator": map[string]interface{}{
			"propertyName": "command",
		},
	}
}
