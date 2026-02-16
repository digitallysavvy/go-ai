package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// TestAnthropicContentBlocksIntegration tests that content blocks flow correctly through conversion
func TestAnthropicContentBlocksIntegration(t *testing.T) {
	// Create messages with tool results containing content blocks
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Search for math tools"},
			},
		},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "I'll search for math tools"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ContentResult("call_123", "tool_search",
					types.TextContentBlock{Text: "Found 3 math tools:"},
					ToolReference("add"),
					ToolReference("multiply"),
					ToolReference("divide"),
				),
			},
		},
	}

	// Convert to Anthropic format
	anthropicMessages := prompt.ToAnthropicMessages(messages)

	// Should have 2 messages (assistant + user with tool result)
	// System messages are filtered out by ToAnthropicMessages
	if len(anthropicMessages) < 2 {
		t.Fatalf("Expected at least 2 Anthropic messages, got %d", len(anthropicMessages))
	}

	// Find the message with tool_result (should be in the last message with user role)
	var toolMessage map[string]interface{}
	var toolResultBlock map[string]interface{}

	for _, msg := range anthropicMessages {
		if content, ok := msg["content"].([]map[string]interface{}); ok {
			for _, block := range content {
				if block["type"] == "tool_result" {
					toolMessage = msg
					toolResultBlock = block
					break
				}
			}
			if toolMessage != nil {
				break
			}
		}
	}

	if toolMessage == nil {
		t.Fatal("Tool message not found")
	}

	if toolResultBlock == nil {
		t.Fatal("Tool result block not found")
	}


	// Verify tool_use_id
	if toolResultBlock["tool_use_id"] != "call_123" {
		t.Errorf("Expected tool_use_id 'call_123', got %v", toolResultBlock["tool_use_id"])
	}

	// Verify content array inside tool_result
	contentArray, ok := toolResultBlock["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("Tool result should have content array, got %T", toolResultBlock["content"])
	}

	if len(contentArray) != 4 { // 1 text + 3 tool references
		t.Fatalf("Expected 4 content blocks, got %d", len(contentArray))
	}

	// Verify first block is text
	if contentArray[0]["type"] != "text" {
		t.Errorf("First block should be text, got %v", contentArray[0]["type"])
	}
	if contentArray[0]["text"] != "Found 3 math tools:" {
		t.Errorf("Unexpected text: %v", contentArray[0]["text"])
	}

	// Verify tool references
	expectedTools := []string{"add", "multiply", "divide"}
	for i, expectedTool := range expectedTools {
		block := contentArray[i+1]
		if block["type"] != "tool_reference" {
			t.Errorf("Block %d should be tool_reference, got %v", i+1, block["type"])
		}
		if block["tool_name"] != expectedTool {
			t.Errorf("Block %d: expected tool_name %s, got %v", i+1, expectedTool, block["tool_name"])
		}
	}
}

// TestAnthropicMixedContentIntegration tests mixed content types
func TestAnthropicMixedContentIntegration(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	fileData := []byte{0x25, 0x50, 0x44, 0x46}  // PDF header

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Analyze this data"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ContentResult("call_456", "analyze",
					types.TextContentBlock{Text: "Analysis complete"},
					types.ImageContentBlock{
						Data:      imageData,
						MediaType: "image/png",
					},
					types.FileContentBlock{
						Data:      fileData,
						MediaType: "application/pdf",
						Filename:  "report.pdf",
					},
				),
			},
		},
	}

	anthropicMessages := prompt.ToAnthropicMessages(messages)

	// Find tool result
	var toolResultBlock map[string]interface{}
	for _, msg := range anthropicMessages {
		if content, ok := msg["content"].([]map[string]interface{}); ok {
			for _, block := range content {
				if block["type"] == "tool_result" {
					toolResultBlock = block
					break
				}
			}
		}
	}

	if toolResultBlock == nil {
		t.Fatal("Tool result not found")
	}

	// Verify content array
	contentArray, ok := toolResultBlock["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("Tool result should have content array")
	}

	if len(contentArray) != 3 {
		t.Fatalf("Expected 3 content blocks, got %d", len(contentArray))
	}

	// Verify text block
	if contentArray[0]["type"] != "text" {
		t.Errorf("First block should be text, got %v", contentArray[0]["type"])
	}

	// Verify image block
	if contentArray[1]["type"] != "image" {
		t.Errorf("Second block should be image, got %v", contentArray[1]["type"])
	}
	imageSource := contentArray[1]["source"].(map[string]interface{})
	if imageSource["type"] != "base64" {
		t.Errorf("Image source type should be base64")
	}
	if imageSource["media_type"] != "image/png" {
		t.Errorf("Image media_type should be image/png")
	}

	// Verify document block
	if contentArray[2]["type"] != "document" {
		t.Errorf("Third block should be document, got %v", contentArray[2]["type"])
	}
	docSource := contentArray[2]["source"].(map[string]interface{})
	if docSource["type"] != "base64" {
		t.Errorf("Document source type should be base64")
	}
	if docSource["media_type"] != "application/pdf" {
		t.Errorf("Document media_type should be application/pdf")
	}
}

// TestAnthropicBackwardCompatibilityIntegration tests old-style results still work
func TestAnthropicBackwardCompatibilityIntegration(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Calculate 2+2"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.SimpleTextResult("call_789", "calculate", "4"),
			},
		},
	}

	anthropicMessages := prompt.ToAnthropicMessages(messages)

	// Find tool result
	var toolResultBlock map[string]interface{}
	for _, msg := range anthropicMessages {
		if content, ok := msg["content"].([]map[string]interface{}); ok {
			for _, block := range content {
				if block["type"] == "tool_result" {
					toolResultBlock = block
					break
				}
			}
		}
	}

	if toolResultBlock == nil {
		t.Fatal("Tool result not found")
	}

	// Old-style should have content as string, not array
	content := toolResultBlock["content"]
	if contentStr, ok := content.(string); !ok {
		t.Errorf("Old-style result should have string content, got %T", content)
	} else if contentStr != "4" {
		t.Errorf("Expected content '4', got %v", contentStr)
	}
}
