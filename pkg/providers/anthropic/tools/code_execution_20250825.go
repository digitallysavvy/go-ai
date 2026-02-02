package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CodeExecution20250825InputType represents the input type discriminator
type CodeExecution20250825InputType string

const (
	InputTypeProgrammaticToolCall       CodeExecution20250825InputType = "programmatic-tool-call"
	InputTypeBashCodeExecution          CodeExecution20250825InputType = "bash_code_execution"
	InputTypeTextEditorCodeExecution    CodeExecution20250825InputType = "text_editor_code_execution"
)

// CodeExecution20250825TextEditorCommand represents text editor command types
type CodeExecution20250825TextEditorCommand string

const (
	TextEditorCommandView       CodeExecution20250825TextEditorCommand = "view"
	TextEditorCommandCreate     CodeExecution20250825TextEditorCommand = "create"
	TextEditorCommandStrReplace CodeExecution20250825TextEditorCommand = "str_replace"
)

// CodeExecution20250825 creates a code execution tool that enables Claude to run code
// and manipulate files in a secure, sandboxed environment.
//
// This tool supports:
//   - Python code execution (programmatic tool calling)
//   - Bash command execution
//   - Text editor operations (view, create, str_replace)
//
// Claude can:
//   - Analyze data
//   - Create visualizations
//   - Perform complex calculations
//   - Run system commands
//   - Create and edit files
//   - Process uploaded files
//
// This is the latest version (v20250825) with enhanced Bash support and file operations.
//
// The tool supports deferred results for programmatic tool calling, where code execution
// can trigger client-executed tools that need to be resolved before the code execution
// result can be returned.
//
// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
//
// Example:
//
//	tool := anthropicTools.CodeExecution20250825()
func CodeExecution20250825() types.Tool {
	// The input schema is a discriminated union supporting three types:
	// 1. programmatic-tool-call: Python code that may trigger client tools
	// 2. bash_code_execution: Shell commands
	// 3. text_editor_code_execution: File operations (view, create, str_replace)
	parameters := buildCodeExecution20250825Schema()

	tool := types.Tool{
		Name: "anthropic.code_execution_20250825",
		Description: `Code execution tool for running Python and Bash code in a secure, sandboxed environment.

This tool enables Claude to:
- Execute Python code for data analysis, visualizations, and calculations
- Run Bash commands for system operations
- Create and edit files using text editor commands
- Process uploaded files directly within the conversation

Input Types:
1. Programmatic Tool Calling (type: "programmatic-tool-call")
   - Execute Python code with 'code' parameter
   - Can trigger client-executed tools via allowedCallers
   - Results may be deferred until client tools are resolved

2. Bash Code Execution (type: "bash_code_execution")
   - Execute shell commands with 'command' parameter
   - Returns stdout, stderr, and return_code
   - Can generate output files

3. Text Editor Code Execution (type: "text_editor_code_execution")
   - view: View file contents (requires 'path')
   - create: Create file with content (requires 'path', optional 'file_text')
   - str_replace: Replace strings in file (requires 'path', 'old_str', 'new_str')

Output Types:
- code_execution_result: Python execution result
- bash_code_execution_result: Bash execution result
- text_editor_code_execution_view_result: File view result
- text_editor_code_execution_create_result: File creation result
- text_editor_code_execution_str_replace_result: String replacement result
- bash_code_execution_tool_result_error: Bash execution error
- text_editor_code_execution_tool_result_error: Text editor error

Error codes: invalid_tool_input, unavailable, too_many_requests, execution_time_exceeded,
output_file_too_large, file_not_found

Important: This tool must be executed by the Anthropic API, not locally.`,
		Parameters:       parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("code execution tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}

func buildCodeExecution20250825Schema() map[string]interface{} {
	// Build a discriminated union schema
	// This matches the TypeScript implementation with three input types
	schema := map[string]interface{}{
		"type": "object",
		"oneOf": []interface{}{
			// 1. Programmatic tool calling
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":        "string",
						"const":       string(InputTypeProgrammaticToolCall),
						"description": "Programmatic tool calling: Execute Python code that may trigger client-executed tools",
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
						"type":        "string",
						"const":       string(InputTypeBashCodeExecution),
						"description": "Execute bash commands",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute.",
					},
				},
				"required": []string{"type", "command"},
			},
			// 3. Text editor code execution (with nested discriminated union for commands)
			buildTextEditorCodeExecutionSchema(),
		},
	}

	// Add discriminator for better schema validation
	schema["discriminator"] = map[string]interface{}{
		"propertyName": "type",
		"mapping": map[string]interface{}{
			string(InputTypeProgrammaticToolCall):    "#/oneOf/0",
			string(InputTypeBashCodeExecution):       "#/oneOf/1",
			string(InputTypeTextEditorCodeExecution): "#/oneOf/2",
		},
	}

	return schema
}

func buildTextEditorCodeExecutionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"oneOf": []interface{}{
			// view command
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":  "string",
						"const": string(InputTypeTextEditorCodeExecution),
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       string(TextEditorCommandView),
						"description": "View file contents",
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
						"const": string(InputTypeTextEditorCodeExecution),
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       string(TextEditorCommandCreate),
						"description": "Create or update a file",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to edit.",
					},
					"file_text": map[string]interface{}{
						"type":        "string",
						"description": "The text of the file to edit.",
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
						"const": string(InputTypeTextEditorCodeExecution),
					},
					"command": map[string]interface{}{
						"type":        "string",
						"const":       string(TextEditorCommandStrReplace),
						"description": "Replace a string in a file",
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
