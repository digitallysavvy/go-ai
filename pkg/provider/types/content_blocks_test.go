package types

import (
	"testing"
)

// TestToolResultOutputTypes tests the output type constants
func TestToolResultOutputTypes(t *testing.T) {
	tests := []struct {
		name     string
		outType  ToolResultOutputType
		expected string
	}{
		{"text type", ToolResultOutputText, "text"},
		{"json type", ToolResultOutputJSON, "json"},
		{"content type", ToolResultOutputContent, "content"},
		{"error type", ToolResultOutputError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.outType) != tt.expected {
				t.Errorf("ToolResultOutputType = %v, want %v", tt.outType, tt.expected)
			}
		})
	}
}

// TestContentBlockTypes tests content block type implementations
func TestContentBlockTypes(t *testing.T) {
	tests := []struct {
		name     string
		block    ToolResultContentBlock
		expected string
	}{
		{
			"text block",
			TextContentBlock{Text: "test"},
			"text",
		},
		{
			"image block",
			ImageContentBlock{Data: []byte{1, 2, 3}, MediaType: "image/png"},
			"image",
		},
		{
			"file block",
			FileContentBlock{Data: []byte{1, 2, 3}, MediaType: "application/pdf"},
			"file",
		},
		{
			"custom block",
			CustomContentBlock{ProviderOptions: map[string]interface{}{"test": "value"}},
			"custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.block.ToolResultContentType() != tt.expected {
				t.Errorf("ToolResultContentType() = %v, want %v",
					tt.block.ToolResultContentType(), tt.expected)
			}
		})
	}
}

// TestSimpleTextResult tests backward compatible simple text result
func TestSimpleTextResult(t *testing.T) {
	result := SimpleTextResult("call_123", "search", "Found 3 results")

	if result.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %v, want call_123", result.ToolCallID)
	}
	if result.ToolName != "search" {
		t.Errorf("ToolName = %v, want search", result.ToolName)
	}
	if result.Result != "Found 3 results" {
		t.Errorf("Result = %v, want 'Found 3 results'", result.Result)
	}
	if result.Output != nil {
		t.Error("Output should be nil for simple result")
	}
}

// TestSimpleJSONResult tests backward compatible JSON result
func TestSimpleJSONResult(t *testing.T) {
	data := map[string]interface{}{"answer": 42}
	result := SimpleJSONResult("call_456", "calculate", data)

	if result.ToolCallID != "call_456" {
		t.Errorf("ToolCallID = %v, want call_456", result.ToolCallID)
	}
	if result.ToolName != "calculate" {
		t.Errorf("ToolName = %v, want calculate", result.ToolName)
	}
	if result.Output != nil {
		t.Error("Output should be nil for simple result")
	}

	// Check the result data
	if resultMap, ok := result.Result.(map[string]interface{}); ok {
		if resultMap["answer"] != 42 {
			t.Errorf("Result[answer] = %v, want 42", resultMap["answer"])
		}
	} else {
		t.Error("Result should be a map")
	}
}

// TestContentResult tests new structured content result
func TestContentResult(t *testing.T) {
	result := ContentResult("call_789", "search",
		TextContentBlock{Text: "Search results:"},
		TextContentBlock{Text: "Found 3 items"},
	)

	if result.ToolCallID != "call_789" {
		t.Errorf("ToolCallID = %v, want call_789", result.ToolCallID)
	}
	if result.ToolName != "search" {
		t.Errorf("ToolName = %v, want search", result.ToolName)
	}
	if result.Output == nil {
		t.Fatal("Output should not be nil for content result")
	}
	if result.Output.Type != ToolResultOutputContent {
		t.Errorf("Output.Type = %v, want %v", result.Output.Type, ToolResultOutputContent)
	}
	if len(result.Output.Content) != 2 {
		t.Errorf("Output.Content length = %v, want 2", len(result.Output.Content))
	}

	// Check first block
	if block, ok := result.Output.Content[0].(TextContentBlock); ok {
		if block.Text != "Search results:" {
			t.Errorf("First block text = %v, want 'Search results:'", block.Text)
		}
	} else {
		t.Error("First block should be TextContentBlock")
	}
}

// TestErrorResult tests error result creation
func TestErrorResult(t *testing.T) {
	result := ErrorResult("call_999", "broken_tool", "Network timeout")

	if result.ToolCallID != "call_999" {
		t.Errorf("ToolCallID = %v, want call_999", result.ToolCallID)
	}
	if result.Error != "Network timeout" {
		t.Errorf("Error = %v, want 'Network timeout'", result.Error)
	}
	if result.Output == nil {
		t.Fatal("Output should not be nil for error result")
	}
	if result.Output.Type != ToolResultOutputError {
		t.Errorf("Output.Type = %v, want %v", result.Output.Type, ToolResultOutputError)
	}
	if result.Output.Value != "Network timeout" {
		t.Errorf("Output.Value = %v, want 'Network timeout'", result.Output.Value)
	}
}

// TestMixedContentBlocks tests combining different content block types
func TestMixedContentBlocks(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	fileData := []byte{0x25, 0x50, 0x44, 0x46}  // PDF header

	result := ContentResult("call_abc", "analyze",
		TextContentBlock{Text: "Analysis complete"},
		ImageContentBlock{
			Data:      imageData,
			MediaType: "image/png",
		},
		FileContentBlock{
			Data:      fileData,
			MediaType: "application/pdf",
			Filename:  "report.pdf",
		},
	)

	if len(result.Output.Content) != 3 {
		t.Fatalf("Expected 3 content blocks, got %d", len(result.Output.Content))
	}

	// Verify text block
	textBlock, ok := result.Output.Content[0].(TextContentBlock)
	if !ok {
		t.Fatal("First block should be TextContentBlock")
	}
	if textBlock.Text != "Analysis complete" {
		t.Errorf("Text block content = %v, want 'Analysis complete'", textBlock.Text)
	}

	// Verify image block
	imageBlock, ok := result.Output.Content[1].(ImageContentBlock)
	if !ok {
		t.Fatal("Second block should be ImageContentBlock")
	}
	if imageBlock.MediaType != "image/png" {
		t.Errorf("Image block media type = %v, want 'image/png'", imageBlock.MediaType)
	}
	if len(imageBlock.Data) != len(imageData) {
		t.Errorf("Image block data length = %v, want %v", len(imageBlock.Data), len(imageData))
	}

	// Verify file block
	fileBlock, ok := result.Output.Content[2].(FileContentBlock)
	if !ok {
		t.Fatal("Third block should be FileContentBlock")
	}
	if fileBlock.MediaType != "application/pdf" {
		t.Errorf("File block media type = %v, want 'application/pdf'", fileBlock.MediaType)
	}
	if fileBlock.Filename != "report.pdf" {
		t.Errorf("File block filename = %v, want 'report.pdf'", fileBlock.Filename)
	}
}

// TestCustomContentBlock tests custom content with provider options
func TestCustomContentBlock(t *testing.T) {
	custom := CustomContentBlock{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"type":     "tool-reference",
				"toolName": "calculator",
			},
		},
	}

	if custom.ToolResultContentType() != "custom" {
		t.Errorf("ContentType = %v, want 'custom'", custom.ToolResultContentType())
	}

	// Verify provider options
	anthropicOpts, ok := custom.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatal("anthropic provider options should be a map")
	}

	if anthropicOpts["type"] != "tool-reference" {
		t.Errorf("type = %v, want 'tool-reference'", anthropicOpts["type"])
	}
	if anthropicOpts["toolName"] != "calculator" {
		t.Errorf("toolName = %v, want 'calculator'", anthropicOpts["toolName"])
	}
}

// TestProviderOptionsOnAllBlocks tests that all blocks support provider options
func TestProviderOptionsOnAllBlocks(t *testing.T) {
	opts := map[string]interface{}{"custom": "data"}

	// Text block
	textBlock := TextContentBlock{
		Text:            "test",
		ProviderOptions: opts,
	}
	if textBlock.ProviderOptions["custom"] != "data" {
		t.Error("TextContentBlock provider options not preserved")
	}

	// Image block
	imageBlock := ImageContentBlock{
		Data:            []byte{1},
		MediaType:       "image/png",
		ProviderOptions: opts,
	}
	if imageBlock.ProviderOptions["custom"] != "data" {
		t.Error("ImageContentBlock provider options not preserved")
	}

	// File block
	fileBlock := FileContentBlock{
		Data:            []byte{1},
		MediaType:       "application/pdf",
		ProviderOptions: opts,
	}
	if fileBlock.ProviderOptions["custom"] != "data" {
		t.Error("FileContentBlock provider options not preserved")
	}
}

// TestBackwardCompatibility tests that old and new styles coexist
func TestBackwardCompatibility(t *testing.T) {
	// Old style - should still work
	oldResult := ToolResultContent{
		ToolCallID: "call_old",
		ToolName:   "old_tool",
		Result:     "simple text",
	}

	if oldResult.ContentType() != "tool-result" {
		t.Error("Old style should still have correct content type")
	}
	if oldResult.Output != nil {
		t.Error("Old style should not have Output set")
	}

	// New style
	newResult := ContentResult("call_new", "new_tool",
		TextContentBlock{Text: "structured content"},
	)

	if newResult.ContentType() != "tool-result" {
		t.Error("New style should have correct content type")
	}
	if newResult.Output == nil {
		t.Error("New style should have Output set")
	}

	// Both should implement ContentPart
	var oldPart ContentPart = oldResult
	var newPart ContentPart = newResult

	if oldPart.ContentType() != "tool-result" {
		t.Error("Old style doesn't implement ContentPart correctly")
	}
	if newPart.ContentType() != "tool-result" {
		t.Error("New style doesn't implement ContentPart correctly")
	}
}
