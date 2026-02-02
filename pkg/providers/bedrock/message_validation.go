package bedrock

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ValidateMessage validates a message structure for Bedrock API compatibility.
// This ensures that messages meet the requirements for Bedrock's Converse API,
// especially for multi-modal content (text, images, files).
//
// Validation rules:
//   - Message role must not be empty
//   - Message must have at least one content part
//   - Text content must not be empty
//   - Image content must have either Image data or URL
//   - File content must have Data
//
// Returns an error if validation fails, nil otherwise.
func ValidateMessage(msg types.Message) error {
	// Check role is not empty
	if msg.Role == "" {
		return fmt.Errorf("message role is required")
	}

	// Check content is not empty
	if len(msg.Content) == 0 {
		return fmt.Errorf("message content cannot be empty (role: %s)", msg.Role)
	}

	// Validate each content part
	for i, content := range msg.Content {
		if err := validateContentPart(content, i, msg.Role); err != nil {
			return err
		}
	}

	return nil
}

// ValidateMessages validates a slice of messages.
// Returns an error for the first invalid message found.
func ValidateMessages(messages []types.Message) error {
	for i, msg := range messages {
		if err := ValidateMessage(msg); err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
	}
	return nil
}

// validateContentPart validates a single content part within a message.
func validateContentPart(content types.ContentPart, index int, role types.MessageRole) error {
	if content == nil {
		return fmt.Errorf("content at index %d is nil (role: %s)", index, role)
	}

	contentType := content.ContentType()
	if contentType == "" {
		return fmt.Errorf("content at index %d has empty type (role: %s)", index, role)
	}

	switch contentType {
	case "text":
		// Text content must not be empty
		if textContent, ok := content.(types.TextContent); ok {
			if textContent.Text == "" {
				return fmt.Errorf("text content at index %d is empty (role: %s)", index, role)
			}
		}

	case "image":
		// Image content must have either data or URL
		if imageContent, ok := content.(types.ImageContent); ok {
			if err := validateImageContent(imageContent, index, role); err != nil {
				return err
			}
		}

	case "file":
		// File content must have data
		if fileContent, ok := content.(types.FileContent); ok {
			if err := validateFileContent(fileContent, index, role); err != nil {
				return err
			}
		}

	case "tool-result":
		// Tool result content - validate it has required fields
		if toolContent, ok := content.(types.ToolResultContent); ok {
			if toolContent.ToolCallID == "" {
				return fmt.Errorf("tool result content at index %d missing tool call ID (role: %s)", index, role)
			}
			if toolContent.ToolName == "" {
				return fmt.Errorf("tool result content at index %d missing tool name (role: %s)", index, role)
			}
		}

	case "reasoning":
		// Reasoning content - validate it has text
		if reasoningContent, ok := content.(types.ReasoningContent); ok {
			if reasoningContent.Text == "" {
				return fmt.Errorf("reasoning content at index %d is empty (role: %s)", index, role)
			}
		}

	default:
		// Unknown content type - allow it but the Bedrock API will reject if invalid
	}

	return nil
}

// validateImageContent validates image content structure.
func validateImageContent(content types.ImageContent, index int, role types.MessageRole) error {
	// Image must have either data or URL
	hasData := len(content.Image) > 0
	hasURL := content.URL != ""

	if !hasData && !hasURL {
		return fmt.Errorf("image content at index %d must have either Image data or URL (role: %s)", index, role)
	}

	// If has data, must have MIME type
	if hasData && content.MimeType == "" {
		return fmt.Errorf("image content at index %d with Image data missing MimeType (role: %s)", index, role)
	}

	return nil
}

// validateFileContent validates file content structure.
func validateFileContent(content types.FileContent, index int, role types.MessageRole) error {
	// File must have data
	if len(content.Data) == 0 {
		return fmt.Errorf("file content at index %d has empty Data (role: %s)", index, role)
	}

	// Must have MIME type
	if content.MimeType == "" {
		return fmt.Errorf("file content at index %d missing MimeType (role: %s)", index, role)
	}

	return nil
}
