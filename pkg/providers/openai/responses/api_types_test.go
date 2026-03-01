package responses

import (
	"encoding/json"
	"testing"
)

// Tests for AssistantMessageItem Phase field round-trip (present and absent)

func TestAssistantMessageItem_Phase_Present(t *testing.T) {
	phase := "final_answer"
	original := AssistantMessageItem{
		Type:  "message",
		Role:  "assistant",
		ID:    "msg_001",
		Phase: &phase,
		Content: []AssistantMessageContent{
			{Type: "output_text", Text: "Here is the answer."},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got AssistantMessageItem
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Phase == nil {
		t.Fatal("expected phase to be present")
	}
	if *got.Phase != "final_answer" {
		t.Errorf("phase: got %q, want %q", *got.Phase, "final_answer")
	}
	if got.ID != "msg_001" {
		t.Errorf("ID: got %q, want %q", got.ID, "msg_001")
	}
	if len(got.Content) != 1 || got.Content[0].Text != "Here is the answer." {
		t.Error("content mismatch")
	}
}

func TestAssistantMessageItem_Phase_Absent(t *testing.T) {
	original := AssistantMessageItem{
		Type: "message",
		Role: "assistant",
		ID:   "msg_002",
		Content: []AssistantMessageContent{
			{Type: "output_text", Text: "No phase here."},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify phase is omitted in serialized JSON
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if _, ok := m["phase"]; ok {
		t.Error("phase field should be omitted when nil")
	}

	// Verify round-trip
	var got AssistantMessageItem
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Phase != nil {
		t.Errorf("expected nil phase, got %q", *got.Phase)
	}
}

func TestAssistantMessageItem_Phase_Commentary(t *testing.T) {
	phase := "commentary"
	item := AssistantMessageItem{
		Type:  "message",
		Role:  "assistant",
		Phase: &phase,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got AssistantMessageItem
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Phase == nil || *got.Phase != "commentary" {
		t.Errorf("phase: got %v, want commentary", got.Phase)
	}
}

func TestAssistantMessageItem_ParseFromAPIResponse(t *testing.T) {
	// Simulate a JSON payload with phase that might come from the API
	payload := `{
		"type": "message",
		"role": "assistant",
		"id": "msg_abc",
		"phase": "final_answer",
		"content": [{"type": "output_text", "text": "done"}]
	}`

	var item AssistantMessageItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if item.Phase == nil || *item.Phase != "final_answer" {
		t.Errorf("phase: got %v, want final_answer", item.Phase)
	}
}

func TestAssistantMessageItem_ParseFromAPIResponse_NoPhase(t *testing.T) {
	// Simulate a JSON payload without phase
	payload := `{
		"type": "message",
		"role": "assistant",
		"id": "msg_xyz",
		"content": [{"type": "output_text", "text": "hello"}]
	}`

	var item AssistantMessageItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if item.Phase != nil {
		t.Errorf("expected nil phase, got %q", *item.Phase)
	}
	if item.Content[0].Text != "hello" {
		t.Error("content mismatch")
	}
}

// Tests for custom tool def serialization

func TestCustomToolDef_JSON_WithGrammar(t *testing.T) {
	syntax := "lark"
	def := `start: WORD`
	original := CustomToolDef{
		Type: "custom",
		Name: "json-tool",
		Format: &CustomToolDefFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &def,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CustomToolDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "custom" {
		t.Errorf("Type: got %q", got.Type)
	}
	if got.Name != "json-tool" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.Format == nil {
		t.Fatal("Format should not be nil")
	}
	if got.Format.Type != "grammar" {
		t.Errorf("Format.Type: got %q", got.Format.Type)
	}
}

// Tests for CustomToolCallOutput

func TestCustomToolCallOutput_StringOutput(t *testing.T) {
	original := CustomToolCallOutput{
		Type:   "custom_tool_call_output",
		CallID: "call_123",
		Output: "The sentiment is positive.",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if m["type"] != "custom_tool_call_output" {
		t.Errorf("type: got %v, want custom_tool_call_output", m["type"])
	}
	if m["call_id"] != "call_123" {
		t.Errorf("call_id: got %v", m["call_id"])
	}
	if m["output"] != "The sentiment is positive." {
		t.Errorf("output: got %v", m["output"])
	}
}

func TestCustomToolCallOutput_MultiPartOutput(t *testing.T) {
	original := CustomToolCallOutput{
		Type:   "custom_tool_call_output",
		CallID: "call_456",
		Output: []CustomToolCallOutputPart{
			{Type: "input_text", Text: "Result: done"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	parts, ok := m["output"].([]interface{})
	if !ok || len(parts) != 1 {
		t.Fatalf("expected output array with 1 item, got %v", m["output"])
	}
	part := parts[0].(map[string]interface{})
	if part["type"] != "input_text" {
		t.Errorf("part type: got %v, want input_text", part["type"])
	}
	if part["text"] != "Result: done" {
		t.Errorf("part text: got %v", part["text"])
	}
}

func TestCustomToolCallOutputPart_OmitEmpty(t *testing.T) {
	part := CustomToolCallOutputPart{
		Type: "input_text",
		Text: "hello",
		// ImageURL, Filename, FileData should be omitted
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := m["image_url"]; ok {
		t.Error("image_url should be omitted when empty")
	}
	if _, ok := m["filename"]; ok {
		t.Error("filename should be omitted when empty")
	}
	if _, ok := m["file_data"]; ok {
		t.Error("file_data should be omitted when empty")
	}
}

func TestCustomToolDef_JSON_NoFormat(t *testing.T) {
	original := CustomToolDef{
		Type: "custom",
		Name: "simple-tool",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if _, ok := m["format"]; ok {
		t.Error("format should be omitted when nil")
	}
	if _, ok := m["description"]; ok {
		t.Error("description should be omitted when nil")
	}
}
