package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// CodeExecution creates a provider-executed tool that runs code in a sandboxed environment.
// Execution is handled by xAI's servers (maps to API type "code_interpreter").
// Returns output and any errors from the code execution.
//
// Example:
//
//	tool := xai.CodeExecution()
func CodeExecution() types.Tool {
	return xaiProviderTool(types.Tool{
		Name:             "xai.code_execution",
		Description:      "Execute code in a sandboxed environment. Returns output and any errors.",
		Parameters:       xaiSchema(xaiNoInputSchemaJSON),
		OutputSchema:     xaiSchema(xaiCodeExecutionOutputSchemaJSON),
		ProviderExecuted: true,
		Execute:          providerExecutedNoop("xai.code_execution"),
	}, nil)
}
