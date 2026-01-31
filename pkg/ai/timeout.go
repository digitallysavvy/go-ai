package ai

import (
	"context"
	"time"
)

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
//	}
type TimeoutConfig struct {
	// Total is the overall timeout for the entire operation
	// If set, the operation will fail if it exceeds this duration
	Total *time.Duration

	// PerStep is the timeout for each generation step
	// Applies to tool calling loops and multi-step agent operations
	// If a single step exceeds this duration, the operation fails
	PerStep *time.Duration

	// PerChunk is the timeout for receiving each chunk in streaming operations
	// If no chunk is received within this duration, the stream is aborted
	// This helps detect stalled streams and network issues
	PerChunk *time.Duration
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
