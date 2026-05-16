package types

import (
	"encoding/json"
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

// TestCustomContentType verifies CustomContent implements ContentPart and
// round-trips cleanly through encoding/json.
func TestCustomContentType(t *testing.T) {
	// ContentType()
	c := CustomContent{
		Kind:             "xai-citation",
		ProviderMetadata: json.RawMessage(`{"url":"https://example.com","title":"Example"}`),
	}
	if c.ContentType() != "custom" {
		t.Errorf("ContentType() = %q, want \"custom\"", c.ContentType())
	}

	// Verify it implements ContentPart
	var _ ContentPart = c

	// Marshal
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal CustomContent failed: %v", err)
	}

	// Unmarshal round-trip
	var got CustomContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal CustomContent failed: %v", err)
	}
	if got.Kind != c.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, c.Kind)
	}
	// ProviderMetadata must be preserved byte-for-byte (json.RawMessage round-trip)
	if string(got.ProviderMetadata) != string(c.ProviderMetadata) {
		t.Errorf("ProviderMetadata = %s, want %s", got.ProviderMetadata, c.ProviderMetadata)
	}
}

// TestCustomContentNoMetadata verifies CustomContent marshals correctly when
// ProviderMetadata is absent (omitempty).
func TestCustomContentNoMetadata(t *testing.T) {
	c := CustomContent{Kind: "xai-unknown"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got CustomContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got.Kind != "xai-unknown" {
		t.Errorf("Kind = %q, want \"xai-unknown\"", got.Kind)
	}
	if got.ProviderMetadata != nil {
		t.Errorf("ProviderMetadata should be nil, got %s", got.ProviderMetadata)
	}
}

// TestReasoningFileContentBase64 verifies that ReasoningFileContent.Data
// ([]byte) is automatically base64-encoded by encoding/json on marshal and
// decoded back to the original bytes on unmarshal — no explicit base64 handling
// needed in application code.
func TestReasoningFileContentBase64(t *testing.T) {
	original := []byte("PNG\x89\x50\x4E\x47\x0D\x0A\x1A\x0A")
	r := ReasoningFileContent{
		MediaType: "image/png",
		Data:      original,
	}

	if r.ContentType() != "reasoning-file" {
		t.Errorf("ContentType() = %q, want \"reasoning-file\"", r.ContentType())
	}

	// Verify it implements ContentPart
	var _ ContentPart = r

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal ReasoningFileContent failed: %v", err)
	}

	// data field must be a JSON string (base64), not a JSON array.
	// Verify by checking the outer JSON contains a "data" key with a string value.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to parse marshaled JSON: %v", err)
	}
	dataField, ok := raw["data"]
	if !ok {
		t.Fatal("marshaled JSON missing 'data' field")
	}
	// A base64 string starts with '"'
	if len(dataField) == 0 || dataField[0] != '"' {
		t.Errorf("'data' field should be a JSON string (base64), got: %s", dataField)
	}

	// Unmarshal round-trip
	var got ReasoningFileContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got.MediaType != r.MediaType {
		t.Errorf("MediaType = %q, want %q", got.MediaType, r.MediaType)
	}
	if len(got.Data) != len(original) {
		t.Fatalf("Data length = %d, want %d", len(got.Data), len(original))
	}
	for i, b := range original {
		if got.Data[i] != b {
			t.Errorf("Data[%d] = %02x, want %02x", i, got.Data[i], b)
		}
	}
}

// TestReasoningFileContentBinary verifies that arbitrary binary data
// (including zero bytes) round-trips correctly.
func TestReasoningFileContentBinary(t *testing.T) {
	binary := make([]byte, 256)
	for i := range binary {
		binary[i] = byte(i)
	}
	r := ReasoningFileContent{
		MediaType:        "application/octet-stream",
		Data:             binary,
		ProviderMetadata: json.RawMessage(`{"source":"model"}`),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got ReasoningFileContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(got.Data) != len(binary) {
		t.Fatalf("Data length = %d, want %d", len(got.Data), len(binary))
	}
	for i, b := range binary {
		if got.Data[i] != b {
			t.Errorf("Data[%d] = %02x, want %02x", i, got.Data[i], b)
		}
	}
	if string(got.ProviderMetadata) != `{"source":"model"}` {
		t.Errorf("ProviderMetadata = %s, want {\"source\":\"model\"}", got.ProviderMetadata)
	}
}

// TestGenerateResultContainsCustomContent verifies that GenerateResult.Content
// can hold CustomContent parts.
func TestGenerateResultContainsCustomContent(t *testing.T) {
	result := GenerateResult{
		Text: "hello",
		Content: []ContentPart{
			CustomContent{
				Kind:             "xai-citation",
				ProviderMetadata: json.RawMessage(`{"url":"https://x.ai"}`),
			},
		},
	}

	if len(result.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(result.Content))
	}
	cc, ok := result.Content[0].(CustomContent)
	if !ok {
		t.Fatal("Content[0] should be CustomContent")
	}
	if cc.Kind != "xai-citation" {
		t.Errorf("Kind = %q, want \"xai-citation\"", cc.Kind)
	}
}

// TestGenerateResultContainsReasoningFile verifies that GenerateResult.Content
// can hold ReasoningFileContent parts.
func TestGenerateResultContainsReasoningFile(t *testing.T) {
	result := GenerateResult{
		Text: "here is a chart",
		Content: []ContentPart{
			ReasoningFileContent{
				MediaType: "image/png",
				Data:      []byte{0x89, 0x50, 0x4E, 0x47},
			},
		},
	}

	if len(result.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(result.Content))
	}
	rf, ok := result.Content[0].(ReasoningFileContent)
	if !ok {
		t.Fatal("Content[0] should be ReasoningFileContent")
	}
	if rf.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want \"image/png\"", rf.MediaType)
	}
}

// TestSourceContentURL verifies SourceContent for a URL source.
func TestSourceContentURL(t *testing.T) {
	s := SourceContent{
		SourceType: "url",
		ID:         "src-1",
		URL:        "https://example.com/article",
		Title:      "Example Article",
	}
	if s.ContentType() != "source" {
		t.Errorf("ContentType() = %q, want \"source\"", s.ContentType())
	}

	// Verify it implements ContentPart
	var _ ContentPart = s

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var got SourceContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got.SourceType != "url" {
		t.Errorf("SourceType = %q, want \"url\"", got.SourceType)
	}
	if got.URL != "https://example.com/article" {
		t.Errorf("URL = %q, want \"https://example.com/article\"", got.URL)
	}
	if got.Title != "Example Article" {
		t.Errorf("Title = %q, want \"Example Article\"", got.Title)
	}
}

// TestSourceContentDocument verifies SourceContent for a document source.
func TestSourceContentDocument(t *testing.T) {
	s := SourceContent{
		SourceType:       "document",
		ID:               "doc-1",
		MediaType:        "application/pdf",
		Title:            "Research Paper",
		Filename:         "paper.pdf",
		ProviderMetadata: json.RawMessage(`{"pages":42}`),
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var got SourceContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got.SourceType != "document" {
		t.Errorf("SourceType = %q, want \"document\"", got.SourceType)
	}
	if got.MediaType != "application/pdf" {
		t.Errorf("MediaType = %q, want \"application/pdf\"", got.MediaType)
	}
	if got.Filename != "paper.pdf" {
		t.Errorf("Filename = %q, want \"paper.pdf\"", got.Filename)
	}
	if string(got.ProviderMetadata) != `{"pages":42}` {
		t.Errorf("ProviderMetadata = %s, want {\"pages\":42}", got.ProviderMetadata)
	}
}

// TestGeneratedFileContent verifies GeneratedFileContent base64 round-trip.
func TestGeneratedFileContent(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG magic bytes
	f := GeneratedFileContent{
		MediaType: "image/png",
		Data:      data,
	}
	if f.ContentType() != "file" {
		t.Errorf("ContentType() = %q, want \"file\"", f.ContentType())
	}

	// Verify it implements ContentPart
	var _ ContentPart = f

	encoded, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// data field must use the TypeScript tagged file-data shape.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("failed to parse marshaled JSON: %v", err)
	}
	if raw["data"][0] != '{' {
		t.Errorf("data field should be tagged file-data object, got: %s", raw["data"])
	}
	var tagged struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw["data"], &tagged); err != nil {
		t.Fatalf("failed to parse tagged data: %v", err)
	}
	if tagged.Type != "data" || tagged.Data == "" {
		t.Fatalf("tagged data = %#v, want type=data with base64 data", tagged)
	}

	var got GeneratedFileContent
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want \"image/png\"", got.MediaType)
	}
	for i, b := range data {
		if got.Data[i] != b {
			t.Errorf("Data[%d] = %02x, want %02x", i, got.Data[i], b)
		}
	}
}

func TestGeneratedFileContentURLUsesTaggedDataShape(t *testing.T) {
	f := GeneratedFileContent{
		MediaType: "image/png",
		FileData:  FileData{Type: FileDataTypeURL, URL: "https://example.com/out.png", MediaType: "image/png"},
		URL:       "https://example.com/out.png",
	}
	encoded, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("failed to parse marshaled JSON: %v", err)
	}
	if _, ok := raw["url"]; ok {
		t.Fatalf("top-level url should not be emitted in TS file shape: %s", encoded)
	}
	if _, ok := raw["fileData"]; ok {
		t.Fatalf("top-level fileData should not be emitted in TS file shape: %s", encoded)
	}
	var data struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("failed to parse tagged data: %v", err)
	}
	if data.Type != "url" || data.URL != "https://example.com/out.png" {
		t.Fatalf("tagged data = %#v", data)
	}
}

// TestCustomContentProviderOptions verifies that CustomContent.ProviderOptions
// is preserved through JSON round-trip (input/prompt direction).
func TestCustomContentProviderOptions(t *testing.T) {
	c := CustomContent{
		Kind: "anthropic-tool-reference",
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"type":     "tool-reference",
				"toolName": "calc",
			},
		},
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got CustomContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if got.Kind != c.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, c.Kind)
	}
	if got.ProviderOptions == nil {
		t.Fatal("ProviderOptions should not be nil after round-trip")
	}
	anthropicOpts, ok := got.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatal("ProviderOptions[\"anthropic\"] should be a map")
	}
	if anthropicOpts["type"] != "tool-reference" {
		t.Errorf("type = %v, want \"tool-reference\"", anthropicOpts["type"])
	}
}

// TestReasoningFileContentProviderOptions verifies that ReasoningFileContent.ProviderOptions
// is preserved through JSON round-trip (input/prompt direction).
func TestReasoningFileContentProviderOptions(t *testing.T) {
	r := ReasoningFileContent{
		MediaType: "image/png",
		Data:      []byte{0x89, 0x50, 0x4E, 0x47},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"cacheControl": "ephemeral"},
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got ReasoningFileContent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if got.MediaType != r.MediaType {
		t.Errorf("MediaType = %q, want %q", got.MediaType, r.MediaType)
	}
	if got.ProviderOptions == nil {
		t.Fatal("ProviderOptions should not be nil after round-trip")
	}
	anthropicOpts, ok := got.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatal("ProviderOptions[\"anthropic\"] should be a map")
	}
	if anthropicOpts["cacheControl"] != "ephemeral" {
		t.Errorf("cacheControl = %v, want \"ephemeral\"", anthropicOpts["cacheControl"])
	}
}

// TestGenerateResultContainsSourceContent verifies GenerateResult.Content
// can hold SourceContent parts.
func TestGenerateResultContainsSourceContent(t *testing.T) {
	result := GenerateResult{
		Text: "See references.",
		Content: []ContentPart{
			SourceContent{SourceType: "url", ID: "s1", URL: "https://x.ai"},
			SourceContent{SourceType: "url", ID: "s2", URL: "https://docs.x.ai"},
		},
	}
	if len(result.Content) != 2 {
		t.Fatalf("Content length = %d, want 2", len(result.Content))
	}
	src, ok := result.Content[0].(SourceContent)
	if !ok {
		t.Fatal("Content[0] should be SourceContent")
	}
	if src.URL != "https://x.ai" {
		t.Errorf("URL = %q, want \"https://x.ai\"", src.URL)
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
