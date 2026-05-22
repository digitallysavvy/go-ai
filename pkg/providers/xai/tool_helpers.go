package xai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// providerExecutedNoop returns an Execute function for provider-executed tools.
// These tools are run by xAI's servers; local execution reports provider execution.
func providerExecutedNoop(toolName string) func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
	return func(_ context.Context, _ map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return nil, &types.ToolExecutionError{
			ToolCallID:       opts.ToolCallID,
			ToolName:         toolName,
			Err:              context.Canceled,
			ProviderExecuted: true,
		}
	}
}
