package prompt

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPromptUtilityHelpers(t *testing.T) {
	msgs := SimpleTextToMessages("hello")
	if len(msgs) != 1 || msgs[0].Role != types.RoleUser {
		t.Fatalf("SimpleTextToMessages() = %#v", msgs)
	}
	if got := MessagesToSimpleText([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
	}); got != "a\nb" {
		t.Fatalf("MessagesToSimpleText() = %q, want %q", got, "a\nb")
	}

	withTools := AddToolResultsToMessages(msgs, []types.ToolResult{
		{ToolCallID: "call-1", ToolName: "lookup", Result: map[string]interface{}{"ok": true}},
	})
	if len(withTools) != 2 || withTools[1].Role != types.RoleTool {
		t.Fatalf("AddToolResultsToMessages() appended message = %#v", withTools)
	}
	if got := ExtractSystemMessage([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "u"}}},
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "s"}}},
	}); got != "s" {
		t.Fatalf("ExtractSystemMessage() = %q, want %q", got, "s")
	}
}

func TestValidateMessages(t *testing.T) {
	if err := ValidateMessages(nil); err == nil {
		t.Fatal("ValidateMessages(nil) should fail")
	}
	if err := ValidateMessages([]types.Message{{Role: "", Content: []types.ContentPart{types.TextContent{Text: "x"}}}}); err == nil {
		t.Fatal("ValidateMessages(empty role) should fail")
	}
	if err := ValidateMessages([]types.Message{{Role: types.RoleUser}}); err == nil {
		t.Fatal("ValidateMessages(empty content) should fail")
	}
	if err := ValidateMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "ok"}}}}); err != nil {
		t.Fatalf("ValidateMessages(valid) error = %v", err)
	}
}

func TestToolResultAndFilePartHelpers(t *testing.T) {
	if got := openAIToolResultText(types.ToolResultContent{
		ToolName: "x",
		Output: &types.ToolResultOutput{
			Type:    types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "content-text"}},
		},
	}); got != "content-text" {
		t.Fatalf("openAIToolResultText(text output) = %q", got)
	}
	if got := openAIToolResultText(types.ToolResultContent{
		ToolName: "x",
		Output: &types.ToolResultOutput{
			Type:    types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{types.ImageContentBlock{MediaType: "image/png", Data: []byte{1}}},
		},
	}); got == "" {
		t.Fatal("openAIToolResultText(complex output) should return fallback")
	}
	if got := openAIToolResultText(types.ToolResultContent{
		Result: map[string]interface{}{"ok": true},
	}); got == "" {
		t.Fatal("openAIToolResultText(result fallback) should not be empty")
	}

	imagePart := openAIFileContentPart(types.FileContent{
		Data:      []byte{0x01, 0x02},
		MediaType: "image/png",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"imageDetail": "high"},
		},
	})
	if imagePart["type"] != "image_url" {
		t.Fatalf("openAI image part type = %v", imagePart["type"])
	}
	imageURL := imagePart["image_url"].(map[string]interface{})
	if imageURL["detail"] != "high" {
		t.Fatalf("image detail = %v, want high", imageURL["detail"])
	}

	filePart := openAIFileContentPart(types.FileContent{
		Reference: "file_123",
		MediaType: "application/pdf",
	})
	fileMap := filePart["file"].(map[string]interface{})
	if fileMap["file_id"] != "file_123" {
		t.Fatalf("openAI file_id = %v", fileMap["file_id"])
	}

	anthropic := anthropicFileContentPart(types.FileContent{
		Text:      "doc body",
		MediaType: "text/plain",
	})
	if anthropic["type"] != "document" {
		t.Fatalf("anthropic part type = %v", anthropic["type"])
	}
	source := anthropic["source"].(map[string]interface{})
	if source["type"] != "text" || source["data"] != "doc body" {
		t.Fatalf("anthropic text source = %#v", source)
	}

	blockPart := anthropicFileContentBlockPart(types.FileContentBlock{
		Data:      []byte("abc"),
		MediaType: "text/plain",
	})
	blockSource := blockPart["source"].(map[string]interface{})
	if blockSource["data"] != base64.StdEncoding.EncodeToString([]byte("abc")) {
		t.Fatalf("anthropic block data = %v", blockSource["data"])
	}

	googleURL := googleFileContentPart(types.FileContent{
		URL:       "https://example.com/a.png",
		MediaType: "image/png",
	})
	if googleURL["fileData"] == nil {
		t.Fatalf("google URL fileData missing: %#v", googleURL)
	}
	googleRef := googleFileContentBlockPart(types.FileContentBlock{
		Reference: "gs://bucket/file",
		MediaType: "application/json",
	})
	if googleRef["fileData"] == nil {
		t.Fatalf("google reference fileData missing: %#v", googleRef)
	}
}
