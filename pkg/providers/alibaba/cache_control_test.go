package alibaba

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestCacheControlOnSystemMessage verifies cache_control is applied to system messages.
func TestCacheControlOnSystemMessage(t *testing.T) {
	validator := NewCacheControlValidator()
	msgs := []types.Message{
		{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}},
		},
	}

	result := ConvertToAlibabaChatMessages(msgs, validator)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	msg := result[0]
	if msg["role"] != "system" {
		t.Errorf("role = %v, want system", msg["role"])
	}

	contentArr, ok := msg["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{} when cacheControl is set, got %T", msg["content"])
	}
	if len(contentArr) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(contentArr))
	}
	if contentArr[0]["cache_control"] == nil {
		t.Errorf("expected cache_control in system message content, got nil")
	}
}

// TestCacheControlOnUserMessage verifies cache_control is applied to the last user content part.
func TestCacheControlOnUserMessage(t *testing.T) {
	validator := NewCacheControlValidator()
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello world"},
			},
		},
	}

	result := ConvertToAlibabaChatMessages(msgs, validator)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	msg := result[0]
	if msg["role"] != "user" {
		t.Errorf("role = %v, want user", msg["role"])
	}

	// With cache control, user message should use content array
	contentArr, ok := msg["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{} when cacheControl is set, got %T", msg["content"])
	}
	if len(contentArr) == 0 {
		t.Fatal("expected at least 1 content part")
	}

	lastPart := contentArr[len(contentArr)-1]
	if lastPart["cache_control"] == nil {
		t.Errorf("expected cache_control on last user content part, got nil")
	}
}

// TestCacheControlOnAssistantMessage verifies cache_control is applied to assistant messages.
func TestCacheControlOnAssistantMessage(t *testing.T) {
	validator := NewCacheControlValidator()
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "I can help with that."},
			},
		},
	}

	result := ConvertToAlibabaChatMessages(msgs, validator)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	msg := result[0]
	if msg["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", msg["role"])
	}

	// With cache control, assistant message should use content array
	contentArr, ok := msg["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{} when cacheControl is set, got %T", msg["content"])
	}
	if len(contentArr) == 0 {
		t.Fatal("expected at least 1 content part")
	}
	if contentArr[0]["cache_control"] == nil {
		t.Errorf("expected cache_control on assistant content, got nil")
	}
}

// TestCacheControlOnToolMessage verifies cache_control is applied to tool messages.
func TestCacheControlOnToolMessage(t *testing.T) {
	validator := NewCacheControlValidator()
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call_abc",
					ToolName:   "search",
					Result:     "result text",
				},
			},
		},
	}

	result := ConvertToAlibabaChatMessages(msgs, validator)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	msg := result[0]
	if msg["role"] != "tool" {
		t.Errorf("role = %v, want tool", msg["role"])
	}

	// With cache control, tool message should use content array
	contentArr, ok := msg["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{} when cacheControl is set, got %T", msg["content"])
	}
	if len(contentArr) == 0 {
		t.Fatal("expected at least 1 content part")
	}
	if contentArr[0]["cache_control"] == nil {
		t.Errorf("expected cache_control on tool message content, got nil")
	}
}

// TestNoCacheControlProducesStringContent verifies that without cache control,
// simple messages use the compact string form for content.
func TestNoCacheControlProducesStringContent(t *testing.T) {
	msgs := []types.Message{
		{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: "Be concise."}},
		},
		{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "Hello"}},
		},
	}

	result := ConvertToAlibabaChatMessages(msgs, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	for _, msg := range result {
		if _, ok := msg["content"].(string); !ok {
			t.Errorf("without cacheControl, expected content to be string for role=%v, got %T", msg["role"], msg["content"])
		}
	}
}

// TestCacheControlStructure verifies the ephemeral cache control has type="ephemeral".
func TestCacheControlStructure(t *testing.T) {
	cc := NewEphemeralCacheControl()
	if cc == nil {
		t.Fatal("NewEphemeralCacheControl() returned nil")
	}
	if cc.Type != "ephemeral" {
		t.Errorf("Type = %q, want %q", cc.Type, "ephemeral")
	}

	// Verify JSON serialization
	b, err := json.Marshal(cc)
	if err != nil {
		t.Fatalf("json.Marshal(cacheControl) error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if m["type"] != "ephemeral" {
		t.Errorf("serialized type = %v, want ephemeral", m["type"])
	}
}

// TestBuildRequestBodyWithCacheControl verifies that buildRequestBody applies
// cache control when specified in provider options.
func TestBuildRequestBodyWithCacheControl(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "Be helpful.",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
			},
		},
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"cacheControl": true,
			},
		},
	}

	body := model.buildRequestBody(opts, false)

	messages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected messages to be []map[string]interface{}, got %T", body["messages"])
	}
	if len(messages) == 0 {
		t.Fatal("expected at least 1 message")
	}

	// System message should have cache control
	sysMsg := messages[0]
	if sysMsg["role"] != "system" {
		t.Errorf("first message role = %v, want system", sysMsg["role"])
	}
	if _, ok := sysMsg["content"].([]map[string]interface{}); !ok {
		t.Errorf("expected system message content to be array with cacheControl, got %T", sysMsg["content"])
	}
}
