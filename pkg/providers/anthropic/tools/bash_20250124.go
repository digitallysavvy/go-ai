package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// BashSandboxOutput is the default output of the Anthropic bash tools when
// they run through a providerutils.SandboxSession (TS: the result of
// `sandbox.run({command, abortSignal})`, `{exitCode, stdout, stderr}`).
type BashSandboxOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// executeBash is the default execute of the Anthropic bash tools. As TS, it
// runs the command through the call's sandbox session
// (ToolExecutionOptions.ExperimentalSandbox, a providerutils.SandboxSession)
// and fails with "Sandbox session is not available" without one. Sandboxes
// with a v0.5 `Execute(ctx, command, opts)` method and a RuntimeContext with
// ExecuteCommand are still used.
//
// A nil pointer (or other nil value) held in ExperimentalSandbox or
// RuntimeContext counts as no sandbox, as TS `!sandbox` treats null and
// undefined: its methods are never called on the nil receiver.
func executeBash(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
	command, _ := input["command"].(string)
	if isNilSandbox(options.ExperimentalSandbox) {
		options.ExperimentalSandbox = nil
	}
	if isNilSandbox(options.RuntimeContext) {
		options.RuntimeContext = nil
	}
	if session, ok := options.ExperimentalSandbox.(providerutils.SandboxSession); ok {
		result, err := session.Run(ctx, providerutils.SandboxProcessOptions{Command: command})
		if err != nil {
			return nil, err
		}
		return BashSandboxOutput{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, nil
	}
	_, legacySandbox := options.RuntimeContext.(bashCommandSandbox)
	if hasLegacyExecute(options.ExperimentalSandbox) || legacySandbox {
		if command == "" {
			return nil, fmt.Errorf("command is required")
		}
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
	return nil, errors.New("Sandbox session is not available") //nolint:staticcheck // matches TS SDK's exact error text
}

// isNilSandbox reports whether v is nil or an interface holding a nil
// pointer, map, slice, func, chan or interface value.
func isNilSandbox(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

type bashCommandSandbox interface {
	ExecuteCommand(ctx context.Context, command string, restart bool) (interface{}, error)
}

// legacyExecuteMethod returns sandbox's v0.5 `Execute(ctx, command, opts)`
// method, if it has one.
func legacyExecuteMethod(sandbox interface{}) (reflect.Value, bool) {
	if sandbox == nil {
		return reflect.Value{}, false
	}
	method := reflect.ValueOf(sandbox).MethodByName("Execute")
	if !method.IsValid() {
		return reflect.Value{}, false
	}
	mt := method.Type()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if mt.NumIn() != 3 || mt.NumOut() != 2 ||
		!mt.In(0).Implements(contextType) ||
		mt.In(1).Kind() != reflect.String ||
		!mt.Out(1).Implements(errorType) {
		return reflect.Value{}, false
	}
	return method, true
}

func hasLegacyExecute(sandbox interface{}) bool {
	_, ok := legacyExecuteMethod(sandbox)
	return ok
}

func executeWithSandbox(ctx context.Context, sandbox interface{}, command string) (interface{}, bool, error) {
	method, ok := legacyExecuteMethod(sandbox)
	if !ok {
		return nil, false, nil
	}
	mt := method.Type()
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
		Name:         "anthropic.bash_20250124",
		Type:         types.ToolTypeProviderDefined,
		ProviderID:   "anthropic.bash_20250124",
		ProviderArgs: noProviderArgs(),
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
		Execute:    executeBash,
	}

	return tool
}
