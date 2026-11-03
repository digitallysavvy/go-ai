package prompt

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r86genPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// TS convertPartToLanguageModelPart detects the image media type of inline
// data given as bytes or as a base64 string (detectMediaType takes both), in
// user file parts and tool-result file content. Expected values: TS
// convertToLanguageModelPrompt at 30548ee7f9 run under bun on the same
// prompt: [["image/png"],["tool-call"],[["image/png"]]], and
// "InvalidCharacterError: The string contains invalid characters." for a
// string that is not base64.
func TestR86genSniffBase64StringData(t *testing.T) {
	data := types.FileData{Type: types.FileDataTypeData, DataString: r86genPNGBase64}
	got, err := NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{FileData: data, MediaType: "image"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{
			Type:    types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{types.FileContentBlock{FileData: data, MediaType: "image/jpeg"}},
		}}}},
	}}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f := got.Messages[0].Content[0].(types.FileContent); f.MediaType != "image/png" {
		t.Errorf("user file media type = %q, want image/png", f.MediaType)
	}
	block := got.Messages[2].Content[0].(types.ToolResultContent).Output.Content[0].(types.FileContentBlock)
	if block.MediaType != "image/png" {
		t.Errorf("tool-result file media type = %q, want image/png", block.MediaType)
	}

	_, err = NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "!!!notbase64"}, MediaType: "image"}}},
	}}, false, nil, nil)
	if err == nil {
		t.Fatal("want an error for a string that is not base64")
	}
}
