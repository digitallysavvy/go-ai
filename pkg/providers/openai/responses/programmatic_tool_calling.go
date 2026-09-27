// Programmatic tool calling for the OpenAI Responses API (row 1f6dd3a).
//
// Programmatic tool calling lets the model generate and execute JavaScript
// that calls declared function tools programmatically instead of emitting
// one function call per turn. Declare NewProgrammaticToolCallingTool()
// alongside function tools that should be callable this way; those function
// tools must also opt in via providerOptions.openai.allowedCallers
// containing "programmatic" (see functionToolAllowedCallers in
// prepare_tools.go).
//
// The API returns a "program" output item (code + fingerprint) followed by a
// "program_output" item (result + status); both decode into a single
// provider-executed tool call/result pair named
// "openai.programmatic_tool_calling".
package responses

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// NewProgrammaticToolCallingTool creates a types.Tool that enables OpenAI's
// hosted programmatic tool calling.
//
// Example:
//
//	tool := responses.NewProgrammaticToolCallingTool()
func NewProgrammaticToolCallingTool() types.Tool {
	return types.Tool{
		Name:             "openai.programmatic_tool_calling",
		Description:      "Generate and execute JavaScript that calls declared function tools programmatically",
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("programmatic tool calling is executed by the OpenAI API, not locally")
		},
	}
}
