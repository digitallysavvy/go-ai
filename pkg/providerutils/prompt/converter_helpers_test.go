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
	// TS's contentValue switch JSON.stringifies output.value (the whole
	// block array) for the "content" case, identically to "json"/"error-json"
	// -- not a first-text-block extraction (convert-to-openai-chat-messages.ts
	// and convert-to-openai-compatible-chat-messages.ts).
	if got := openAIToolResultText(types.ToolResultContent{
		ToolName: "x",
		Output: &types.ToolResultOutput{
			Type:    types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "content-text"}},
		},
	}); got != `[{"type":"text","text":"content-text"}]` {
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
	}, false)
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
	}, false)
	fileMap := filePart["file"].(map[string]interface{})
	if fileMap["file_id"] != "file_123" {
		t.Fatalf("openAI file_id = %v", fileMap["file_id"])
	}

	// video/* falls back to the generic "file" shape when AllowVideo is
	// false (OpenAI's own chat converter has no video support).
	noVideoPart := openAIFileContentPart(types.FileContent{
		URL:       "https://example.com/video.mp4",
		MediaType: "video/mp4",
	}, false)
	if noVideoPart["type"] != "file" {
		t.Fatalf("openAI video part type (AllowVideo=false) = %v, want file", noVideoPart["type"])
	}

	// video/* becomes "video_url" when AllowVideo is true (7dd9ec320c,
	// @ai-sdk/openai-compatible's convertToOpenAICompatibleChatMessages).
	videoURLPart := openAIFileContentPart(types.FileContent{
		URL:       "https://example.com/video.mp4",
		MediaType: "video/mp4",
	}, true)
	if videoURLPart["type"] != "video_url" {
		t.Fatalf("openAI video part type (AllowVideo=true) = %v, want video_url", videoURLPart["type"])
	}
	videoURLMap := videoURLPart["video_url"].(map[string]interface{})
	if videoURLMap["url"] != "https://example.com/video.mp4" {
		t.Fatalf("video_url.url = %v", videoURLMap["url"])
	}

	videoDataPart := openAIFileContentPart(types.FileContent{
		Data:      []byte{0x01, 0x02, 0x03},
		MediaType: "video/mp4",
	}, true)
	videoDataMap := videoDataPart["video_url"].(map[string]interface{})
	wantVideoDataURL := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	if videoDataMap["url"] != wantVideoDataURL {
		t.Fatalf("video_url.url (inline data) = %v, want %v", videoDataMap["url"], wantVideoDataURL)
	}

	c := &anthropicConverter{validator: NewAnthropicCacheControlValidator(), betaSet: map[string]bool{}}
	anthropic, err := c.convertUserFile(types.FileContent{
		Text:      "doc body",
		MediaType: "text/plain",
	}, nil)
	if err != nil {
		t.Fatalf("convertUserFile: %v", err)
	}
	if anthropic["type"] != "document" {
		t.Fatalf("anthropic part type = %v", anthropic["type"])
	}
	source := anthropic["source"].(map[string]interface{})
	if source["type"] != "text" || source["data"] != "doc body" {
		t.Fatalf("anthropic text source = %#v", source)
	}

	blockPart, err := c.convertToolResultContentBlock(types.FileContentBlock{
		Data:      []byte("%PDF-1.4"),
		MediaType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("convertToolResultContentBlock: %v", err)
	}
	blockSource := blockPart["source"].(map[string]interface{})
	if blockSource["data"] != base64.StdEncoding.EncodeToString([]byte("%PDF-1.4")) {
		t.Fatalf("anthropic block data = %v", blockSource["data"])
	}

	gc := &googleConverter{names: []string{"google"}}
	googleURL, _ := gc.fileContentPart(types.FileContent{
		URL:       "https://example.com/a.png",
		MediaType: "image/png",
	}, false)
	if googleURL["fileData"] == nil {
		t.Fatalf("google URL fileData missing: %#v", googleURL)
	}
	googleRef, _ := gc.fileContentPart(fileBlockToContent(types.FileContentBlock{
		Reference: "gs://bucket/file",
		MediaType: "application/json",
	}), false)
	if googleRef["fileData"] == nil {
		t.Fatalf("google reference fileData missing: %#v", googleRef)
	}
}
