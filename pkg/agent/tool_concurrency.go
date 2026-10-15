package agent

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/internal/ordered"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// validateToolCallConcurrency mirrors TS prepareToolCallConcurrency.
func validateToolCallConcurrency(n *int) error {
	if n != nil && *n < 1 {
		return &providererrors.InvalidArgumentError{
			Field:   "toolCallConcurrency",
			Message: "toolCallConcurrency must be >= 1",
		}
	}
	return nil
}

// runToolCalls runs runCall for every tool call of a step in the legacy
// AgentResult loop. When AgentConfig.ToolCallConcurrency is unset the calls run
// sequentially, as this loop always has; when set, at most that many run at
// once (mirrors TS mapWithConcurrency applied to tool execution). Results are
// returned in call order regardless of completion order, and once a call fails
// or ctx is cancelled no further queued call is started.
func (a *ToolLoopAgent) runToolCalls(ctx context.Context, toolCalls []types.ToolCall, runCall func(types.ToolCall) ([]types.ToolResult, error)) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, 0, len(toolCalls))
	if a.config.ToolCallConcurrency == nil || len(toolCalls) <= 1 {
		for _, call := range toolCalls {
			res, err := runCall(call)
			results = append(results, res...)
			if err != nil {
				return results, err
			}
		}
		return results, nil
	}
	perCall := make([][]types.ToolResult, len(toolCalls))
	errs := make([]error, len(toolCalls))
	started := ordered.Run(ctx, len(toolCalls), *a.config.ToolCallConcurrency, func(i int) error {
		perCall[i], errs[i] = runCall(toolCalls[i])
		return errs[i]
	})
	notStarted := false
	for i := range toolCalls {
		if !started[i] {
			notStarted = true
			continue
		}
		results = append(results, perCall[i]...)
	}
	for _, err := range errs {
		if err != nil {
			return results, err
		}
	}
	if notStarted && ctx != nil && ctx.Err() != nil {
		return results, ctx.Err()
	}
	return results, nil
}
