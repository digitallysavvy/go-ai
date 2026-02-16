package bedrock

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestValidateMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   types.Message
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_text_message",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Hello, world!"},
				},
			},
			wantError: false,
		},
		{
			name: "empty_role",
			message: types.Message{
				Role: "",
				Content: []types.ContentPart{
					types.TextContent{Text: "Hello"},
				},
			},
			wantError: true,
			errorMsg:  "message role is required",
		},
		{
			name: "empty_content",
			message: types.Message{
				Role:    types.RoleUser,
				Content: []types.ContentPart{},
			},
			wantError: true,
			errorMsg:  "message content cannot be empty",
		},
		{
			name: "empty_text_content",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: ""},
				},
			},
			wantError: true,
			errorMsg:  "text content at index 0 is empty",
		},
		{
			name: "valid_image_with_data",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{
						Image:    []byte("fake-image-data"),
						MimeType: "image/png",
					},
				},
			},
			wantError: false,
		},
		{
			name: "valid_image_with_url",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{
						URL: "https://example.com/image.png",
					},
				},
			},
			wantError: false,
		},
		{
			name: "image_with_no_data_or_url",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{
						MimeType: "image/png",
					},
				},
			},
			wantError: true,
			errorMsg:  "image content at index 0 must have either Image data or URL",
		},
		{
			name: "image_with_data_missing_mimetype",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{
						Image:    []byte("fake-image-data"),
						MimeType: "",
					},
				},
			},
			wantError: true,
			errorMsg:  "image content at index 0 with Image data missing MimeType",
		},
		{
			name: "valid_file_content",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Data:     []byte("fake-file-data"),
						MimeType: "application/pdf",
						Filename: "document.pdf",
					},
				},
			},
			wantError: false,
		},
		{
			name: "file_missing_data",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						MimeType: "application/pdf",
					},
				},
			},
			wantError: true,
			errorMsg:  "file content at index 0 has empty Data",
		},
		{
			name: "file_missing_mimetype",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Data: []byte("fake-file-data"),
					},
				},
			},
			wantError: true,
			errorMsg:  "file content at index 0 missing MimeType",
		},
		{
			name: "valid_tool_result",
			message: types.Message{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call_123",
						ToolName:   "get_weather",
						Result:     map[string]interface{}{"temp": 72},
					},
				},
			},
			wantError: false,
		},
		{
			name: "tool_result_missing_call_id",
			message: types.Message{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolName: "get_weather",
						Result:   map[string]interface{}{"temp": 72},
					},
				},
			},
			wantError: true,
			errorMsg:  "tool result content at index 0 missing tool call ID",
		},
		{
			name: "tool_result_missing_tool_name",
			message: types.Message{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call_123",
						Result:     map[string]interface{}{"temp": 72},
					},
				},
			},
			wantError: true,
			errorMsg:  "tool result content at index 0 missing tool name",
		},
		{
			name: "valid_reasoning_content",
			message: types.Message{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "Let me think about this...",
					},
				},
			},
			wantError: false,
		},
		{
			name: "empty_reasoning_content",
			message: types.Message{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "",
					},
				},
			},
			wantError: true,
			errorMsg:  "reasoning content at index 0 is empty",
		},
		{
			name: "mixed_content_valid",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Check this image:"},
					types.ImageContent{
						Image:    []byte("fake-image-data"),
						MimeType: "image/png",
					},
					types.TextContent{Text: "What do you see?"},
				},
			},
			wantError: false,
		},
		{
			name: "mixed_content_invalid_at_index_1",
			message: types.Message{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Valid text"},
					types.ImageContent{}, // Invalid - no data or URL
					types.TextContent{Text: "More text"},
				},
			},
			wantError: true,
			errorMsg:  "image content at index 1 must have either Image data or URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMessage(tt.message)

			if tt.wantError {
				if err == nil {
					t.Errorf("ValidateMessage() expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateMessage() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateMessage() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateMessages(t *testing.T) {
	tests := []struct {
		name      string
		messages  []types.Message
		wantError bool
		errorMsg  string
	}{
		{
			name: "all_valid",
			messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hi there!"},
					},
				},
			},
			wantError: false,
		},
		{
			name: "second_message_invalid",
			messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.TextContent{Text: ""}, // Invalid
					},
				},
			},
			wantError: true,
			errorMsg:  "message 1:",
		},
		{
			name:      "empty_messages",
			messages:  []types.Message{},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMessages(tt.messages)

			if tt.wantError {
				if err == nil {
					t.Errorf("ValidateMessages() expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateMessages() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateMessages() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateContentPart_NilContent(t *testing.T) {
	msg := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentPart{nil},
	}

	err := ValidateMessage(msg)
	if err == nil {
		t.Error("ValidateMessage() expected error for nil content, got nil")
	}
	if !strings.Contains(err.Error(), "content at index 0 is nil") {
		t.Errorf("ValidateMessage() error = %v, want error containing 'content at index 0 is nil'", err)
	}
}

func TestValidateImageContent(t *testing.T) {
	tests := []struct {
		name      string
		content   types.ImageContent
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_with_data_and_mimetype",
			content: types.ImageContent{
				Image:    []byte("fake-image"),
				MimeType: "image/png",
			},
			wantError: false,
		},
		{
			name: "valid_with_url",
			content: types.ImageContent{
				URL: "https://example.com/image.png",
			},
			wantError: false,
		},
		{
			name: "valid_with_both_data_and_url",
			content: types.ImageContent{
				Image:    []byte("fake-image"),
				MimeType: "image/png",
				URL:      "https://example.com/image.png",
			},
			wantError: false,
		},
		{
			name:      "invalid_no_data_or_url",
			content:   types.ImageContent{},
			wantError: true,
			errorMsg:  "must have either Image data or URL",
		},
		{
			name: "invalid_data_without_mimetype",
			content: types.ImageContent{
				Image: []byte("fake-image"),
			},
			wantError: true,
			errorMsg:  "missing MimeType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImageContent(tt.content, 0, types.RoleUser)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateImageContent() expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateImageContent() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateImageContent() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateFileContent(t *testing.T) {
	tests := []struct {
		name      string
		content   types.FileContent
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid",
			content: types.FileContent{
				Data:     []byte("fake-file"),
				MimeType: "application/pdf",
			},
			wantError: false,
		},
		{
			name: "valid_with_filename",
			content: types.FileContent{
				Data:     []byte("fake-file"),
				MimeType: "application/pdf",
				Filename: "document.pdf",
			},
			wantError: false,
		},
		{
			name: "invalid_no_data",
			content: types.FileContent{
				MimeType: "application/pdf",
			},
			wantError: true,
			errorMsg:  "has empty Data",
		},
		{
			name: "invalid_no_mimetype",
			content: types.FileContent{
				Data: []byte("fake-file"),
			},
			wantError: true,
			errorMsg:  "missing MimeType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFileContent(tt.content, 0, types.RoleUser)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateFileContent() expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateFileContent() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateFileContent() unexpected error = %v", err)
				}
			}
		})
	}
}
