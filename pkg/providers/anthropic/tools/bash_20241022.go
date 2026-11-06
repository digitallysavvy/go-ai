package tools

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Bash20241022 creates a bash tool that enables Claude to execute shell commands
// in a persistent bash session.
//
// This tool allows system operations, script execution, and command-line automation.
// Image results are supported.
//
// Requires beta header: computer-use-2024-10-22 (injected automatically).
//
// Supported models: Claude Sonnet 3.5
//
// Example:
//
//	tool := anthropicTools.Bash20241022()
func Bash20241022() types.Tool {
	return types.Tool{
		Name:         "anthropic.bash_20241022",
		Type:         types.ToolTypeProviderDefined,
		ProviderID:   "anthropic.bash_20241022",
		ProviderArgs: noProviderArgs(),
		Description: `Bash tool for executing shell commands in a persistent bash session.

This enables:
- System operations
- Script execution
- Command-line automation
- File system operations

The bash session persists across multiple tool calls. Image results are supported.

By default this tool executes through the configured ExperimentalSandbox.`,
		Parameters: map[string]interface{}{
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
		},
		Execute: executeBash,
	}
}
