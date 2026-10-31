package agent

import (
	"context"
	"sync"

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

// validateAgentMaxRetries mirrors TS prepareRetries: a negative maxRetries is
// an InvalidArgumentError (nil selects the default of 2).
func validateAgentMaxRetries(n *int) error {
	if n != nil && *n < 0 {
		return &providererrors.InvalidArgumentError{
			Field:   "maxRetries",
			Message: "maxRetries must be >= 0",
		}
	}
	return nil
}

// validateAgentMaxOutputTokens mirrors TS prepareLanguageModelCallOptions: a
// maxOutputTokens below 1 is an InvalidArgumentError.
func validateAgentMaxOutputTokens(n *int) error {
	if n != nil && *n < 1 {
		return &providererrors.InvalidArgumentError{
			Field:   "maxOutputTokens",
			Message: "maxOutputTokens must be >= 1",
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
func (a *ToolLoopAgent) runToolCalls(ctx context.Context, toolCalls []types.ToolCall, runCall func(types.ToolCall, agentToolStartGate) ([]types.ToolResult, error)) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, 0, len(toolCalls))
	limit := 0 // unlimited
	if a.config.ToolCallConcurrency != nil {
		limit = *a.config.ToolCallConcurrency
	}
	if len(toolCalls) <= 1 {
		for _, call := range toolCalls {
			res, err := runCall(call, agentToolStartGate{})
			results = append(results, res...)
			if err != nil {
				return results, err
			}
		}
		return results, nil
	}
	perCall := make([][]types.ToolResult, len(toolCalls))
	errs := make([]error, len(toolCalls))
	// Start callbacks fire in call order, as in ai.executeToolsLimited: call
	// i waits for gate i-1 before firing its own and closes gate i once it
	// has fired them, or on return (after gate i-1, so the chain stays
	// transitive when a call never reaches its start). ordered.Run claims
	// calls in order, so every earlier call is running: no wait deadlocks.
	gates := make([]chan struct{}, len(toolCalls))
	once := make([]sync.Once, len(toolCalls))
	for i := range gates {
		gates[i] = make(chan struct{})
	}
	release := func(i int) { once[i].Do(func() { close(gates[i]) }) }
	started := ordered.Run(ctx, len(toolCalls), limit, func(i int) error {
		gate := agentToolStartGate{done: func() { release(i) }}
		if i > 0 {
			gate.prev = gates[i-1]
		}
		defer func() {
			gate.wait(ctx)
			release(i)
		}()
		perCall[i], errs[i] = runCall(toolCalls[i], gate)
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

// agentToolStartGate orders the start callbacks of a step's parallel tool
// calls. The zero value does not block.
type agentToolStartGate struct {
	prev <-chan struct{}
	done func()
}

// wait blocks until the previous call has fired its start callbacks (or
// returned) or ctx ends.
func (g agentToolStartGate) wait(ctx context.Context) {
	if g.prev == nil {
		return
	}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	select {
	case <-g.prev:
	case <-ctxDone:
	}
}

// fired reports that this call has fired its start callbacks.
func (g agentToolStartGate) fired() {
	if g.done != nil {
		g.done()
	}
}
