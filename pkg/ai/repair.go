package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolCallRepairFunc is a function that attempts to repair a malformed tool call
type ToolCallRepairFunc func(ctx context.Context, toolCall types.ToolCall, err error) (*types.ToolCall, error)

// RepairOptions contains options for tool call repair
type RepairOptions struct {
	// MaxAttempts is the maximum number of repair attempts
	MaxAttempts int

	// RepairFunc is the function to use for repairing tool calls
	RepairFunc ToolCallRepairFunc
}

// DefaultRepairOptions returns default repair options
func DefaultRepairOptions() RepairOptions {
	return RepairOptions{
		MaxAttempts: 3,
		RepairFunc:  DefaultToolCallRepair,
	}
}

// DefaultToolCallRepair is the default tool call repair function
// It attempts to fix common issues with tool call arguments
func DefaultToolCallRepair(ctx context.Context, toolCall types.ToolCall, err error) (*types.ToolCall, error) {
	// Try to fix JSON parsing errors
	if toolCall.Arguments == nil {
		return nil, fmt.Errorf("cannot repair tool call with nil arguments")
	}

	// Attempt to re-parse arguments as JSON string
	var args map[string]interface{}

	// Try parsing as regular JSON first
	jsonBytes, jsonErr := json.Marshal(toolCall.Arguments)
	if jsonErr != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", jsonErr)
	}

	if parseErr := json.Unmarshal(jsonBytes, &args); parseErr == nil {
		return &types.ToolCall{
			ID:        toolCall.ID,
			ToolName:  toolCall.ToolName,
			Arguments: args,
		}, nil
	}

	// Try common fixes
	// 1. Remove trailing commas
	// 2. Fix unquoted keys
	// 3. Escape unescaped quotes

	// For now, return the original error if we can't fix it
	return nil, fmt.Errorf("could not repair tool call: %w", err)
}

// RepairToolCall attempts to repair a malformed tool call
func RepairToolCall(ctx context.Context, toolCall types.ToolCall, err error, opts RepairOptions) (*types.ToolCall, error) {
	if opts.RepairFunc == nil {
		opts.RepairFunc = DefaultToolCallRepair
	}

	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < opts.MaxAttempts; attempt++ {
		repairedCall, repairErr := opts.RepairFunc(ctx, toolCall, err)
		if repairErr == nil {
			return repairedCall, nil
		}
		lastErr = repairErr
	}

	return nil, fmt.Errorf("failed to repair tool call after %d attempts: %w", opts.MaxAttempts, lastErr)
}

// TryRepairToolCalls attempts to repair all tool calls in a slice
func TryRepairToolCalls(ctx context.Context, toolCalls []types.ToolCall, opts RepairOptions) ([]types.ToolCall, error) {
	repaired := make([]types.ToolCall, 0, len(toolCalls))

	for _, tc := range toolCalls {
		// Try to validate the tool call first
		if tc.Arguments == nil {
			// Attempt repair
			repairedCall, err := RepairToolCall(ctx, tc, fmt.Errorf("nil arguments"), opts)
			if err != nil {
				return nil, fmt.Errorf("failed to repair tool call %s: %w", tc.ID, err)
			}
			repaired = append(repaired, *repairedCall)
		} else {
			repaired = append(repaired, tc)
		}
	}

	return repaired, nil
}
