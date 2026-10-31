package ai

import (
	"context"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/internal/ordered"
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
		results, err := executeTools(ctx, calls, tools, runtimeContext, toolsContext, toolApproval, usage, callbacks)
		markDynamicToolResults(results, calls)
		return results, err
	}
	perCallResults := make([][]types.ToolResult, len(calls))
	perCallUsage := make([]types.Usage, len(calls))
	perCallMs := make([]map[string]int64, len(calls))
	errs := make([]error, len(calls))
	// startGates[i] closes once call i has fired its start callbacks, or
	// has returned without firing them (no local execution, a validation
	// or approval outcome, an error, a panic). Call i+1 waits for it before
	// firing its own, so start callbacks fire in call order as in TS;
	// executions still overlap. A call that returns without firing waits
	// for gate i-1 before closing its own, so the chain stays transitive: a
	// later call never fires before an earlier call that is still on its
	// way to its start. ordered.Run claims calls in order, so every call a
	// worker runs has all earlier calls claimed and running: no wait can
	// deadlock.
	startGates := make([]chan struct{}, len(calls))
	startOnce := make([]sync.Once, len(calls))
	for i := range startGates {
		startGates[i] = make(chan struct{})
	}
	releaseStart := func(i int) { startOnce[i].Do(func() { close(startGates[i]) }) }

	started := ordered.Run(ctx, len(calls), limit, func(i int) error {
		defer func() {
			if i > 0 {
				waitToolStartGate(ctx, startGates[i-1])
			}
			releaseStart(i)
		}()
		cb := callbacks
		if callbacks.toolExecutionMs != nil {
			perCallMs[i] = map[string]int64{}
			cb.toolExecutionMs = perCallMs[i]
		}
		if i > 0 {
			cb.startGate = startGates[i-1]
		}
		cb.startDone = func() { releaseStart(i) }
		results, err := executeTools(ctx, []types.ToolCall{calls[i]}, tools, runtimeContext, toolsContext, toolApproval, &perCallUsage[i], cb)
		perCallResults[i] = results
		errs[i] = err
		return err
	})

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
	markDynamicToolResults(out, calls)

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
// ToolCallConcurrency. concurrency == 0 (option not set) executes all calls
// concurrently, as TS does by default; results stay in call order.
func executeToolsConfigured(ctx context.Context, calls []types.ToolCall, tools []types.Tool, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval interface{}, usage *types.Usage, callbacks toolCallEventCallbacks, concurrency int) ([]types.ToolResult, error) {
	return executeToolsLimited(ctx, calls, tools, runtimeContext, toolsContext, toolApproval, usage, callbacks, concurrency)
}

// markDynamicToolResults sets Dynamic on every result whose call was a dynamic
// tool call (TS: a tool-result of a dynamic tool has dynamic: true), so the
// static/dynamic result splits, the content parts and the UI stream agree with
// the call.
func markDynamicToolResults(results []types.ToolResult, calls []types.ToolCall) {
	for i := range results {
		if results[i].Dynamic {
			continue
		}
		for _, call := range calls {
			if call.ID == results[i].ToolCallID {
				results[i].Dynamic = call.Dynamic
				break
			}
		}
	}
}

// waitToolStartGate blocks until gate is closed (the previous parallel call
// fired its start callbacks) or ctx ends. A nil gate does not block.
func waitToolStartGate(ctx context.Context, gate <-chan struct{}) {
	if gate == nil {
		return
	}
	var done <-chan struct{}
	if ctx != nil {
		done = ctx.Done()
	}
	select {
	case <-gate:
	case <-done:
	}
}
