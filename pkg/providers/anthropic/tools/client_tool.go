package tools

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// WithExecute returns a copy of an Anthropic client-executed tool with the
// given Execute function.
//
// The computer, computer toolset, text editor and memory tools run on the
// caller's machine, not on Anthropic's servers: in the TypeScript SDK they are
// ordinary provider tools whose execute function the caller supplies. These
// constructors therefore leave ProviderExecuted false and Execute nil. A tool
// call for such a tool is returned to the caller unexecuted unless Execute is
// set, either through WithExecute or by assigning tool.Execute directly.
//
// Example:
//
//	tool := tools.WithExecute(tools.Memory20250818(), func(ctx context.Context,
//	    input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
//	    return runMemoryCommand(input)
//	})
func WithExecute(tool types.Tool, execute types.ToolExecutor) types.Tool {
	tool.Execute = execute
	return tool
}
