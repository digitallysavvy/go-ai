package prompt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 91 gen tests (TS 2ebc78b654).

// core2-15: TS returns legacy "image-data" / "file-data" tool-result items
// without convertPartToLanguageModelPart (convert-to-language-model-prompt.ts
// 679-692, 746-758), so their declared media type is kept; a "file" item is
// still sniffed.
func TestR91genLegacyToolResultMediaTypeKept(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	var output types.ToolResultOutput
	raw := `{"type":"content","value":[` +
		`{"type":"image-data","data":"` + png + `","mediaType":"image/jpeg"},` +
		`{"type":"file-data","data":"` + png + `","mediaType":"application/octet-stream"},` +
		`{"type":"file","mediaType":"image/jpeg","data":{"type":"data","data":"` + png + `"}}]}`
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		t.Fatal(err)
	}
	prompt := types.Prompt{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `{}`}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c", ToolName: "t", Output: &output}}},
	}}
	got, err := NormalizePromptWithDownloadSupport(context.Background(), prompt, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.Messages[1].Content[0].(types.ToolResultContent)
	if !ok || result.Output == nil {
		t.Fatalf("tool result = %#v", got.Messages[1].Content[0])
	}
	var mediaTypes []string
	for _, block := range result.Output.Content {
		switch b := block.(type) {
		case types.FileContentBlock:
			mediaTypes = append(mediaTypes, b.MediaType)
		case *types.FileContentBlock:
			mediaTypes = append(mediaTypes, b.MediaType)
		}
	}
	want := []string{"image/jpeg", "application/octet-stream", "image/png"}
	if len(mediaTypes) != len(want) {
		t.Fatalf("media types = %v, want %v", mediaTypes, want)
	}
	for i := range want {
		if mediaTypes[i] != want[i] {
			t.Fatalf("media types = %v, want %v", mediaTypes, want)
		}
	}
}
