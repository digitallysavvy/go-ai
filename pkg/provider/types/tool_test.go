package types

import (
	"context"
	"testing"
)

func TestToolChoice_Auto(t *testing.T) {
	t.Parallel()

	tc := AutoToolChoice()

	if tc.Type != ToolChoiceAuto {
		t.Errorf("expected type 'auto', got %s", tc.Type)
	}
	if tc.ToolName != "" {
		t.Errorf("expected empty tool name, got %s", tc.ToolName)
	}
}

func TestToolChoice_None(t *testing.T) {
	t.Parallel()

	tc := NoneToolChoice()

	if tc.Type != ToolChoiceNone {
		t.Errorf("expected type 'none', got %s", tc.Type)
	}
}

func TestToolChoice_Required(t *testing.T) {
	t.Parallel()

	tc := RequiredToolChoice()

	if tc.Type != ToolChoiceRequired {
		t.Errorf("expected type 'required', got %s", tc.Type)
	}
}

func TestToolChoice_Specific(t *testing.T) {
	t.Parallel()

	tc := SpecificToolChoice("my_tool")

	if tc.Type != ToolChoiceTool {
		t.Errorf("expected type 'tool', got %s", tc.Type)
	}
	if tc.ToolName != "my_tool" {
		t.Errorf("expected tool name 'my_tool', got %s", tc.ToolName)
	}
}

func TestToolChoiceType_Constants(t *testing.T) {
	t.Parallel()

	if ToolChoiceAuto != "auto" {
		t.Errorf("expected 'auto', got %s", ToolChoiceAuto)
	}
	if ToolChoiceNone != "none" {
		t.Errorf("expected 'none', got %s", ToolChoiceNone)
	}
	if ToolChoiceRequired != "required" {
		t.Errorf("expected 'required', got %s", ToolChoiceRequired)
	}
	if ToolChoiceTool != "tool" {
		t.Errorf("expected 'tool', got %s", ToolChoiceTool)
	}
}

func TestTool_Execute(t *testing.T) {
	t.Parallel()

	executed := false
	tool := Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			executed = true
			return "result", nil
		},
	}

	if tool.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", tool.Name)
	}
	if tool.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", tool.Description)
	}

	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected execute to be called")
	}
	if result != "result" {
		t.Errorf("expected 'result', got %v", result)
	}
}

func TestToolCall_Fields(t *testing.T) {
	t.Parallel()

	tc := ToolCall{
		ID:        "call_123",
		ToolName:  "my_tool",
		Arguments: map[string]interface{}{"key": "value"},
	}

	if tc.ID != "call_123" {
		t.Errorf("expected ID 'call_123', got %s", tc.ID)
	}
	if tc.ToolName != "my_tool" {
		t.Errorf("expected ToolName 'my_tool', got %s", tc.ToolName)
	}
	if tc.Arguments["key"] != "value" {
		t.Errorf("expected argument 'value', got %v", tc.Arguments["key"])
	}
}

func TestToolResult_Fields(t *testing.T) {
	t.Parallel()

	tr := ToolResult{
		ToolCallID: "call_123",
		ToolName:   "my_tool",
		Result:     "success",
		Error:      nil,
	}

	if tr.ToolCallID != "call_123" {
		t.Errorf("expected ToolCallID 'call_123', got %s", tr.ToolCallID)
	}
	if tr.ToolName != "my_tool" {
		t.Errorf("expected ToolName 'my_tool', got %s", tr.ToolName)
	}
	if tr.Result != "success" {
		t.Errorf("expected Result 'success', got %v", tr.Result)
	}
	if tr.Error != nil {
		t.Errorf("expected nil Error, got %v", tr.Error)
	}
}
