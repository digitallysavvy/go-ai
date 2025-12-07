package types

import (
	"testing"
)

func TestTextContent_ContentType(t *testing.T) {
	t.Parallel()

	tc := TextContent{Text: "Hello"}
	if tc.ContentType() != "text" {
		t.Errorf("expected 'text', got %s", tc.ContentType())
	}
}

func TestImageContent_ContentType(t *testing.T) {
	t.Parallel()

	ic := ImageContent{Image: []byte("fake"), MimeType: "image/png"}
	if ic.ContentType() != "image" {
		t.Errorf("expected 'image', got %s", ic.ContentType())
	}
}

func TestFileContent_ContentType(t *testing.T) {
	t.Parallel()

	fc := FileContent{Data: []byte("fake"), MimeType: "application/pdf"}
	if fc.ContentType() != "file" {
		t.Errorf("expected 'file', got %s", fc.ContentType())
	}
}

func TestToolResultContent_ContentType(t *testing.T) {
	t.Parallel()

	trc := ToolResultContent{ToolCallID: "1", ToolName: "test", Result: "ok"}
	if trc.ContentType() != "tool-result" {
		t.Errorf("expected 'tool-result', got %s", trc.ContentType())
	}
}

func TestPrompt_IsSimple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prompt   Prompt
		expected bool
	}{
		{
			name:     "simple text prompt",
			prompt:   Prompt{Text: "Hello"},
			expected: true,
		},
		{
			name:     "messages prompt",
			prompt:   Prompt{Messages: []Message{{Role: RoleUser}}},
			expected: false,
		},
		{
			name:     "empty prompt",
			prompt:   Prompt{},
			expected: false,
		},
		{
			name:     "text with messages",
			prompt:   Prompt{Text: "Hello", Messages: []Message{{Role: RoleUser}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.prompt.IsSimple(); got != tt.expected {
				t.Errorf("IsSimple() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPrompt_IsMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prompt   Prompt
		expected bool
	}{
		{
			name:     "messages prompt",
			prompt:   Prompt{Messages: []Message{{Role: RoleUser}}},
			expected: true,
		},
		{
			name:     "simple text prompt",
			prompt:   Prompt{Text: "Hello"},
			expected: false,
		},
		{
			name:     "empty prompt",
			prompt:   Prompt{},
			expected: false,
		},
		{
			name:     "empty messages",
			prompt:   Prompt{Messages: []Message{}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.prompt.IsMessages(); got != tt.expected {
				t.Errorf("IsMessages() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMessageRoles(t *testing.T) {
	t.Parallel()

	if RoleSystem != "system" {
		t.Errorf("expected 'system', got %s", RoleSystem)
	}
	if RoleUser != "user" {
		t.Errorf("expected 'user', got %s", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("expected 'assistant', got %s", RoleAssistant)
	}
	if RoleTool != "tool" {
		t.Errorf("expected 'tool', got %s", RoleTool)
	}
}

func TestMessage_Content(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role: RoleUser,
		Content: []ContentPart{
			TextContent{Text: "Hello"},
			ImageContent{MimeType: "image/png"},
		},
		Name: "user1",
	}

	if msg.Role != RoleUser {
		t.Errorf("expected role 'user', got %s", msg.Role)
	}
	if len(msg.Content) != 2 {
		t.Errorf("expected 2 content parts, got %d", len(msg.Content))
	}
	if msg.Name != "user1" {
		t.Errorf("expected name 'user1', got %s", msg.Name)
	}
}
