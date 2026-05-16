package tools

import (
	"context"
	"fmt"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type bashCommandSandbox interface {
	ExecuteCommand(ctx context.Context, command string, restart bool) (interface{}, error)
}

func executeWithSandbox(ctx context.Context, sandbox interface{}, command string) (interface{}, bool, error) {
	if sandbox == nil {
		return nil, false, nil
	}
	method := reflect.ValueOf(sandbox).MethodByName("Execute")
	if !method.IsValid() {
		return nil, false, nil
	}
	mt := method.Type()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if mt.NumIn() != 3 || mt.NumOut() != 2 ||
		!mt.In(0).Implements(contextType) ||
		mt.In(1).Kind() != reflect.String ||
		!mt.Out(1).Implements(errorType) {
		return nil, false, nil
	}
	out := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(command),
		reflect.Zero(mt.In(2)),
	})
	if !out[1].IsNil() {
		return nil, true, out[1].Interface().(error)
	}
	return out[0].Interface(), true, nil
}

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

By default this tool executes through the configured ExperimentalSandbox.`,
		Parameters: parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			command, _ := input["command"].(string)
			if command == "" {
				return nil, fmt.Errorf("command is required")
			}
			if result, ok, err := executeWithSandbox(ctx, options.ExperimentalSandbox, command); ok {
				return result, err
			}
			if sandbox, ok := options.RuntimeContext.(bashCommandSandbox); ok {
				restart := false
				if v, ok := input["restart"].(bool); ok {
					restart = v
				}
				return sandbox.ExecuteCommand(ctx, command, restart)
			}
			return nil, fmt.Errorf("Sandbox is not available")
		},
	}

	return tool
}
