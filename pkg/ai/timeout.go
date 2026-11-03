package ai

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// TimeoutReason identifies which timeout boundary aborted an operation.
type TimeoutReason string

const (
	TimeoutReasonTotal      TimeoutReason = "total"
	TimeoutReasonStep       TimeoutReason = "step"
	TimeoutReasonChunk      TimeoutReason = "chunk"
	TimeoutReasonFirstChunk TimeoutReason = "firstChunk"
	TimeoutReasonTool       TimeoutReason = "tool"
)

// TimeoutError wraps timeout failures so callers can inspect the reason with
// errors.As, matching the TypeScript SDK's tagged timeout failures. Unwrap
// gives the underlying error (usually context.DeadlineExceeded).
type TimeoutError struct {
	Reason TimeoutReason
	Err    error

	// Timeout is the configured duration that elapsed, when known. With it,
	// a step, first-chunk or chunk timeout reads like the TS abort reason
	// ("Chunk timeout of 50ms exceeded").
	Timeout time.Duration
}

// ErrorName is the TS error name (a DOMException named "TimeoutError"), so a
// tool that fails with its timeout reaches the model as TS
// getErrorMessage(error): "TimeoutError: The operation was aborted due to
// timeout" (prompt/create-tool-model-output.ts).
func (e *TimeoutError) ErrorName() string { return "TimeoutError" }

// timeoutLabels are the TS setAbortTimeout labels (stream-text.ts,
// generate-text.ts).
var timeoutLabels = map[TimeoutReason]string{
	TimeoutReasonStep:       "Step",
	TimeoutReasonFirstChunk: "First chunk",
	TimeoutReasonChunk:      "Chunk",
}

// Error is the TS abort reason's message: setAbortTimeout's
// "<Label> timeout of <ms>ms exceeded" for a step, first-chunk or chunk
// timeout with a known duration, and AbortSignal.timeout's "The operation was
// aborted due to timeout" for the total timeout and for a tool timeout with
// a known duration (TS mergeAbortSignals(abortSignal, toolMs)). A timeout
// without a duration (other than total) keeps the Go text.
func (e *TimeoutError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Reason == TimeoutReasonTotal || (e.Reason == TimeoutReasonTool && e.Timeout > 0) {
		return "The operation was aborted due to timeout"
	}
	if label, ok := timeoutLabels[e.Reason]; ok && e.Timeout > 0 {
		ms := strconv.FormatFloat(float64(e.Timeout)/float64(time.Millisecond), 'f', -1, 64)
		return label + " timeout of " + ms + "ms exceeded"
	}
	if e.Err == nil {
		return fmt.Sprintf("%s timeout exceeded", e.Reason)
	}
	return fmt.Sprintf("%s timeout exceeded: %v", e.Reason, e.Err)
}

func (e *TimeoutError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// deadlineFired reports whether ctx ended because a deadline armed on it
// passed while parent was still live: one of this SDK's timeouts, not the
// caller's cancellation or the caller's own deadline. TS tells them apart the
// same way: an expired AbortSignal.timeout rejects with a TimeoutError, an
// abort with the abort reason.
func deadlineFired(ctx, parent context.Context) bool {
	if ctx == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false
	}
	return parent == nil || parent.Err() == nil
}

func wrapTimeoutError(reason TimeoutReason, err error) error {
	if err == nil {
		return nil
	}
	return &TimeoutError{Reason: reason, Err: err}
}

// timeoutError is wrapTimeoutError with the configured duration of the
// timeout that fired.
func (tc *TimeoutConfig) timeoutError(reason TimeoutReason, err error) error {
	if err == nil {
		return nil
	}
	timeoutErr := &TimeoutError{Reason: reason, Err: err}
	if tc != nil {
		var d *time.Duration
		switch reason {
		case TimeoutReasonTotal:
			d = tc.Total
		case TimeoutReasonStep:
			d = tc.PerStep
		case TimeoutReasonFirstChunk:
			d = tc.FirstChunk
		case TimeoutReasonChunk:
			d = tc.PerChunk
		}
		if d != nil {
			timeoutErr.Timeout = *d
		}
	}
	return timeoutErr
}

// TimeoutConfig provides granular timeout controls for AI operations
// Supports total timeouts, per-step timeouts (for multi-step operations),
// and per-chunk timeouts (for streaming operations).
//
// Example:
//
//	timeout := &TimeoutConfig{
//	    Total:    pointer(30 * time.Second),  // Total operation timeout
//	    PerStep:  pointer(10 * time.Second),  // Each generation step
//	    PerChunk: pointer(5 * time.Second),   // Each streaming chunk
//	    ToolMs:   pointer(10 * time.Second),  // Default timeout for all tools
//	    Tools:    map[string]time.Duration{"slowTool": 30 * time.Second},
//	}
type TimeoutConfig struct {
	// Total is the overall timeout for the entire operation
	// If set, the operation will fail if it exceeds this duration
	Total *time.Duration

	// PerStep is the timeout for each generation step
	// Applies to tool calling loops and multi-step agent operations
	// If a single step exceeds this duration, the operation fails
	PerStep *time.Duration

	// PerChunk is the timeout for receiving each *semantic output* chunk in
	// streaming operations (TS chunkMs). Only chunks that carry real output
	// (non-empty text/reasoning/tool-input deltas, files, reasoning files,
	// tool calls — see isOutputChunkForTiming) reset this timer; metadata,
	// empty deltas and block boundary chunks do not, so a burst of
	// non-output chunks can't mask a stalled stream. The timer starts with
	// the first semantic output chunk of each model call attempt (use
	// FirstChunk to bound the wait for it), stops at an error part until
	// the next output chunk, and stops at the model call's finish part (TS
	// clearModelOutputTimeouts on model-call-end); if no further semantic output chunk is received
	// within this duration, the stream is aborted with TimeoutReasonChunk
	// (TS chunkMs; audit row 106ea59 / WG-TIMEOUT).
	PerChunk *time.Duration

	// FirstChunk is the timeout for the first semantic output chunk of each
	// model-call step (TS firstChunkMs). It is armed fresh at the start of
	// every step and disarmed (stops applying, for the rest of that step)
	// as soon as the first chunk matching isOutputChunkForTiming (or an
	// error part or the finish part, TS clearModelOutputTimeouts) arrives;
	// PerChunk (if also set) then takes over from that point. Streaming
	// only, like PerChunk (audit row 106ea59 / WG-TIMEOUT; GenerateText
	// warns instead of applying it — audit row 349afe7).
	FirstChunk *time.Duration

	// ToolMs is the default timeout for all tool executions.
	// If a tool execution exceeds this duration, it is cancelled.
	// Per-tool overrides in Tools take precedence over this value.
	ToolMs *time.Duration //nolint:revive,staticcheck // name matches TS SDK field "toolMs" for parity

	// Tools provides per-tool timeout overrides, keyed by the tool name
	// (e.g. "searchWeb", not "searchWebMs"). When a tool name is found here,
	// this duration is used instead of ToolMs.
	Tools map[string]time.Duration
}

// CreateTimeoutContext creates a context with the appropriate timeout
// based on the TimeoutConfig. Returns the context and a cancel function.
//
// The priority order is:
// 1. Total timeout (if set) - applies to entire operation
// 2. PerStep timeout (if set and step context) - applies to single step
// 3. PerChunk timeout (if set and chunk context) - applies to chunk read
// 4. No timeout - returns original context
func (tc *TimeoutConfig) CreateTimeoutContext(ctx context.Context, timeoutType string) (context.Context, context.CancelFunc) {
	if tc == nil {
		return ctx, func() {}
	}

	switch timeoutType {
	case "total":
		if tc.Total != nil {
			return context.WithTimeout(ctx, *tc.Total)
		}
	case "step":
		if tc.PerStep != nil {
			return context.WithTimeout(ctx, *tc.PerStep)
		}
	case "chunk":
		if tc.PerChunk != nil {
			return context.WithTimeout(ctx, *tc.PerChunk)
		}
	}

	return ctx, func() {}
}

// HasTotal returns true if a total timeout is configured
func (tc *TimeoutConfig) HasTotal() bool {
	return tc != nil && tc.Total != nil
}

// HasPerStep returns true if a per-step timeout is configured
func (tc *TimeoutConfig) HasPerStep() bool {
	return tc != nil && tc.PerStep != nil
}

// HasPerChunk returns true if a per-chunk timeout is configured
func (tc *TimeoutConfig) HasPerChunk() bool {
	return tc != nil && tc.PerChunk != nil
}

// HasFirstChunk returns true if a first-chunk timeout is configured
func (tc *TimeoutConfig) HasFirstChunk() bool {
	return tc != nil && tc.FirstChunk != nil
}

// NewTimeoutConfig creates a new TimeoutConfig with the specified timeouts
// Pass nil for any timeout you don't want to set
func NewTimeoutConfig(total, perStep, perChunk *time.Duration) *TimeoutConfig {
	return &TimeoutConfig{
		Total:    total,
		PerStep:  perStep,
		PerChunk: perChunk,
	}
}

// WithTotal sets the total timeout and returns the config (builder pattern)
func (tc *TimeoutConfig) WithTotal(duration time.Duration) *TimeoutConfig {
	tc.Total = &duration
	return tc
}

// WithPerStep sets the per-step timeout and returns the config (builder pattern)
func (tc *TimeoutConfig) WithPerStep(duration time.Duration) *TimeoutConfig {
	tc.PerStep = &duration
	return tc
}

// WithPerChunk sets the per-chunk timeout and returns the config (builder pattern)
func (tc *TimeoutConfig) WithPerChunk(duration time.Duration) *TimeoutConfig {
	tc.PerChunk = &duration
	return tc
}

// WithFirstChunk sets the first-chunk timeout and returns the config (builder pattern)
func (tc *TimeoutConfig) WithFirstChunk(duration time.Duration) *TimeoutConfig {
	tc.FirstChunk = &duration
	return tc
}

// GetToolTimeout returns the effective timeout for the named tool.
// Per-tool entries in Tools take precedence over ToolMs.
// Returns nil if no timeout applies (tc is nil, ToolMs is nil, and no per-tool entry).
func (tc *TimeoutConfig) GetToolTimeout(toolName string) *time.Duration {
	if tc == nil {
		return nil
	}
	if d, ok := tc.Tools[toolName]; ok {
		return &d
	}
	return tc.ToolMs
}
