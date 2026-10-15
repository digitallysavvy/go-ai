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
// AgentResult loop. At most AgentConfig.ToolCallConcurrency calls run at once;
// when it is unset all calls of the step run concurrently, as in TS (Promise.all
// through mapWithConcurrency) and in ai.GenerateText/StreamText. Set it to 1 for
// sequential execution. Results are returned in call order regardless of
// completion order, and once a call fails or ctx is cancelled no further queued
// call is started.
func (a *ToolLoopAgent) runToolCalls(ctx context.Context, toolCalls []types.ToolCall, runCall func(types.ToolCall) ([]types.ToolResult, error)) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, 0, len(toolCalls))
	limit := 0 // unlimited
	if a.config.ToolCallConcurrency != nil {
		limit = *a.config.ToolCallConcurrency
	}
	if len(toolCalls) <= 1 {
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
	started := ordered.Run(ctx, len(toolCalls), limit, func(i int) error {
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
