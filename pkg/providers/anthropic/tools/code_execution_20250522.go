package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CodeExecution20250522 creates the Anthropic code execution tool (version 2025-05-22).
//
// Claude can analyze data, create visualizations, perform complex calculations, run system
// commands, create and edit files, and process uploaded files in a secure, sandboxed
// environment. This version executes Python code.
//
// Requires beta header: code-execution-2025-05-22 (injected automatically).
//
// Example:
//
//	tool := anthropicTools.CodeExecution20250522()
func CodeExecution20250522() types.Tool {
	return types.Tool{
		Name: "anthropic.code_execution_20250522",
		Description: `Code execution tool that runs Python code in a secure sandboxed environment.

Claude can use this to:
- Analyze data and create visualizations
- Perform complex calculations
- Run system commands
- Create and edit files
- Process uploaded files

The tool executes Python code and returns stdout, stderr, and any generated files.`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code": map[string]interface{}{
					"type":        "string",
					"description": "The Python code to execute.",
				},
			},
			"required": []string{"code"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("code execution tool must be executed by the provider (Anthropic)")
		},
		ProviderExecuted: true,
	}
}
