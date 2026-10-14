package ai

import (
	"context"
	"sync"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// prepareToolCallConcurrency validates the maximum number of tool calls that
// may execute concurrently. A nil value returns 0 (option not set). Mirrors
// TS prepareToolCallConcurrency.
func prepareToolCallConcurrency(toolCallConcurrency *int) (int, error) {
	if toolCallConcurrency == nil {
		return 0, nil
	}
	if *toolCallConcurrency < 1 {
		return 0, &providererrors.InvalidArgumentError{
			Field:   "toolCallConcurrency",
			Message: "toolCallConcurrency must be >= 1",
		}
	}
	return *toolCallConcurrency, nil
}

// executeToolsLimited runs each of calls through executeTools independently
// with at most limit calls executing at once, collecting results in the
// original call order. limit <= 0 means unlimited; limit == 1 executes the
// calls sequentially in order. Workers claim calls in input order; once the
// context is cancelled or an execution fails, no further queued calls are
// started. Each call gets its own *types.Usage accumulator and tool
// execution timing map so concurrent tool Execute functions never race on
// shared state; the totals are merged into usage and
// callbacks.toolExecutionMs once every worker has finished. Mirrors TS
// mapWithConcurrency applied to tool execution.
func executeToolsLimited(ctx context.Context, calls []types.ToolCall, tools []types.Tool, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval interface{}, usage *types.Usage, callbacks toolCallEventCallbacks, limit int) ([]types.ToolResult, error) {
	if len(calls) <= 1 {
		return executeTools(ctx, calls, tools, runtimeContext, toolsContext, toolApproval, usage, callbacks)
	}
	workers := len(calls)
	if limit > 0 && limit < workers {
		workers = limit
	}

	perCallResults := make([][]types.ToolResult, len(calls))
	perCallUsage := make([]types.Usage, len(calls))
	perCallMs := make([]map[string]int64, len(calls))
	errs := make([]error, len(calls))
	started := make([]bool, len(calls))

	var mu sync.Mutex
	next := 0
	stopped := false
	claim := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if stopped || next >= len(calls) || (ctx != nil && ctx.Err() != nil) {
			return 0, false
		}
		i := next
		next++
		started[i] = true
		return i, true
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for {
				i, ok := claim()
				if !ok {
					return
				}
				cb := callbacks
				if callbacks.toolExecutionMs != nil {
					perCallMs[i] = map[string]int64{}
					cb.toolExecutionMs = perCallMs[i]
				}
				results, err := executeTools(ctx, []types.ToolCall{calls[i]}, tools, runtimeContext, toolsContext, toolApproval, &perCallUsage[i], cb)
				perCallResults[i] = results
				errs[i] = err
				if err != nil {
					mu.Lock()
					stopped = true
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()

	merged := usage.Add(types.Usage{})
	out := make([]types.ToolResult, 0, len(calls))
	notStarted := false
	for i := range calls {
		if !started[i] {
			notStarted = true
			continue
		}
		merged = merged.Add(perCallUsage[i])
		out = append(out, perCallResults[i]...)
		for id, ms := range perCallMs[i] {
			callbacks.toolExecutionMs[id] = ms
		}
	}
	*usage = merged

	for _, err := range errs {
		if err != nil {
			return out, err
		}
	}
	if notStarted && ctx != nil && ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, nil
}

// executeToolsConfigured executes tool calls honoring an optional
// ToolCallConcurrency. concurrency == 0 (option not set) keeps the default
// sequential execution.
func executeToolsConfigured(ctx context.Context, calls []types.ToolCall, tools []types.Tool, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval interface{}, usage *types.Usage, callbacks toolCallEventCallbacks, concurrency int) ([]types.ToolResult, error) {
	if concurrency <= 0 {
		return executeTools(ctx, calls, tools, runtimeContext, toolsContext, toolApproval, usage, callbacks)
	}
	return executeToolsLimited(ctx, calls, tools, runtimeContext, toolsContext, toolApproval, usage, callbacks, concurrency)
}
