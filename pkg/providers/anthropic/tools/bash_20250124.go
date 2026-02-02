package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Bash20250124Input represents the input parameters for the bash tool
type Bash20250124Input struct {
	// Command is the bash command to run
	Command string `json:"command"`

	// Restart specifies whether to restart the bash session
	Restart *bool `json:"restart,omitempty"`
}

// Bash20250124 creates a bash tool that enables Claude to execute shell commands
// in a persistent bash session.
//
// This tool allows system operations, script execution, and command-line automation.
// Image results are supported.
//
// This is the latest version (v20250124) with enhanced capabilities.
//
// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
//
// Example:
//
//	tool := anthropicTools.Bash20250124()
func Bash20250124() types.Tool {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The bash command to run. Required unless the tool is being restarted.",
			},
			"restart": map[string]interface{}{
				"type":        "boolean",
				"description": "Specifying true will restart this tool. Otherwise, leave this unspecified.",
			},
		},
		"required": []string{"command"},
	}

	tool := types.Tool{
		Name: "anthropic.bash_20250124",
		Description: `Bash tool for executing shell commands in a persistent bash session.

This enables:
- System operations
- Script execution
- Command-line automation
- File system operations

The bash session persists across multiple tool calls, allowing for stateful operations like changing directories, setting environment variables, and running multi-step commands.

Image results are supported - commands that generate images can return them as part of the result.

Important: This tool must be executed by the Anthropic API, not locally.`,
		Parameters:       parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("bash tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}
