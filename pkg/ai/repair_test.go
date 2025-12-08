package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestRepairToolCall_ValidToolCall(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"key": "value"},
	}

	repaired, err := RepairToolCall(context.Background(), toolCall, nil, DefaultRepairOptions())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired.ID != toolCall.ID {
		t.Errorf("expected ID %s, got %s", toolCall.ID, repaired.ID)
	}
	if repaired.ToolName != toolCall.ToolName {
		t.Errorf("expected ToolName %s, got %s", toolCall.ToolName, repaired.ToolName)
	}
}

func TestRepairToolCall_NilArguments(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: nil,
	}

	_, err := RepairToolCall(context.Background(), toolCall, errors.New("original error"), DefaultRepairOptions())

	if err == nil {
		t.Fatal("expected error for nil arguments")
	}
}

func TestRepairToolCall_MaxAttempts(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: nil,
	}

	opts := RepairOptions{
		MaxAttempts: 3,
		RepairFunc: func(ctx context.Context, tc types.ToolCall, err error) (*types.ToolCall, error) {
			return nil, errors.New("repair always fails")
		},
	}

	_, err := RepairToolCall(context.Background(), toolCall, errors.New("original"), opts)

	if err == nil {
		t.Fatal("expected error after max attempts")
	}
}

func TestRepairToolCall_DefaultRepairFunc(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"valid": "json"},
	}

	repaired, err := DefaultToolCallRepair(context.Background(), toolCall, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired == nil {
		t.Fatal("expected repaired tool call")
	}
}

func TestRepairToolCall_CustomRepairFunc(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"broken": "data"},
	}

	customCalled := false
	opts := RepairOptions{
		MaxAttempts: 1,
		RepairFunc: func(ctx context.Context, tc types.ToolCall, err error) (*types.ToolCall, error) {
			customCalled = true
			return &types.ToolCall{
				ID:        tc.ID,
				ToolName:  tc.ToolName,
				Arguments: map[string]interface{}{"fixed": "data"},
			}, nil
		},
	}

	repaired, err := RepairToolCall(context.Background(), toolCall, nil, opts)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !customCalled {
		t.Error("expected custom repair function to be called")
	}
	if repaired.Arguments["fixed"] != "data" {
		t.Error("expected fixed data")
	}
}

func TestRepairToolCall_ZeroMaxAttempts(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"key": "value"},
	}

	opts := RepairOptions{
		MaxAttempts: 0, // Should default to 1
	}

	repaired, err := RepairToolCall(context.Background(), toolCall, nil, opts)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired == nil {
		t.Fatal("expected repaired tool call")
	}
}

func TestTryRepairToolCalls_AllValid(t *testing.T) {
	t.Parallel()

	toolCalls := []types.ToolCall{
		{ID: "call_1", ToolName: "tool1", Arguments: map[string]interface{}{"a": "b"}},
		{ID: "call_2", ToolName: "tool2", Arguments: map[string]interface{}{"c": "d"}},
	}

	repaired, err := TryRepairToolCalls(context.Background(), toolCalls, DefaultRepairOptions())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repaired) != 2 {
		t.Errorf("expected 2 repaired tool calls, got %d", len(repaired))
	}
}

func TestTryRepairToolCalls_SomeInvalid(t *testing.T) {
	t.Parallel()

	toolCalls := []types.ToolCall{
		{ID: "call_1", ToolName: "tool1", Arguments: map[string]interface{}{"valid": "data"}},
		{ID: "call_2", ToolName: "tool2", Arguments: nil}, // Invalid
	}

	_, err := TryRepairToolCalls(context.Background(), toolCalls, DefaultRepairOptions())

	// Should fail because second tool call has nil arguments
	if err == nil {
		t.Fatal("expected error for invalid tool call")
	}
}

func TestTryRepairToolCalls_Empty(t *testing.T) {
	t.Parallel()

	toolCalls := []types.ToolCall{}

	repaired, err := TryRepairToolCalls(context.Background(), toolCalls, DefaultRepairOptions())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repaired) != 0 {
		t.Errorf("expected 0 repaired tool calls, got %d", len(repaired))
	}
}

func TestDefaultRepairOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultRepairOptions()

	if opts.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts 3, got %d", opts.MaxAttempts)
	}
	if opts.RepairFunc == nil {
		t.Error("expected non-nil RepairFunc")
	}
}

func TestDefaultToolCallRepair_MarshalError(t *testing.T) {
	t.Parallel()

	// Create a tool call with arguments that can be marshaled
	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"key": "value"},
	}

	repaired, err := DefaultToolCallRepair(context.Background(), toolCall, errors.New("parse error"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired.Arguments["key"] != "value" {
		t.Error("expected arguments to be preserved")
	}
}

func TestRepairToolCall_SuccessOnRetry(t *testing.T) {
	t.Parallel()

	attempts := 0
	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"data": "test"},
	}

	opts := RepairOptions{
		MaxAttempts: 3,
		RepairFunc: func(ctx context.Context, tc types.ToolCall, err error) (*types.ToolCall, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("temporary failure")
			}
			return &tc, nil
		},
	}

	repaired, err := RepairToolCall(context.Background(), toolCall, nil, opts)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired == nil {
		t.Fatal("expected repaired tool call")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRepairToolCall_NilRepairFunc(t *testing.T) {
	t.Parallel()

	toolCall := types.ToolCall{
		ID:        "call_1",
		ToolName:  "test_tool",
		Arguments: map[string]interface{}{"key": "value"},
	}

	opts := RepairOptions{
		MaxAttempts: 1,
		RepairFunc:  nil, // Should use default
	}

	repaired, err := RepairToolCall(context.Background(), toolCall, nil, opts)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired == nil {
		t.Fatal("expected repaired tool call")
	}
}
