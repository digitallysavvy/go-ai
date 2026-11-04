package prompt

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR87genRemoveEmptyTextParts ports convert-to-language-model-prompt.test.ts
// "text parts" (user: "should filter out empty text parts", "should pass
// through non-empty text parts"; assistant: "should ignore empty text parts
// when there are no provider options", "should include empty text parts when
// there are provider options").
func TestR87genRemoveEmptyTextParts(t *testing.T) {
	call := types.ToolCallContent{ToolCallID: "toolCallId", ToolName: "toolName", Input: `{}`}
	opts := map[string]interface{}{"test-provider": map[string]interface{}{"key-a": "test-value-1"}}
	toolResult := types.ToolResultContent{ToolCallID: "toolCallId", ToolName: "toolName", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r"}}
	in := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: ""}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello, world!"}, &types.TextContent{Text: "", ProviderOptions: opts}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: ""}, call}},
		{Role: types.RoleTool, Content: []types.ContentPart{toolResult}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "", ProviderOptions: opts}, call}},
		{Role: types.RoleTool, Content: []types.ContentPart{toolResult}},
	}
	before, _ := json.Marshal(in)
	got, err := NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: in}, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("input messages were modified")
	}
	wantLens := []int{0, 1, 1, 1, 2, 1}
	if len(got.Messages) != len(wantLens) {
		t.Fatalf("got %d messages", len(got.Messages))
	}
	for i, m := range got.Messages {
		if len(m.Content) != wantLens[i] {
			t.Errorf("message %d: %d parts, want %d: %+v", i, len(m.Content), wantLens[i], m.Content)
		}
	}
	if tc, ok := got.Messages[1].Content[0].(types.TextContent); !ok || tc.Text != "hello, world!" {
		t.Errorf("message 1 kept %+v", got.Messages[1].Content)
	}
	if _, ok := got.Messages[2].Content[0].(types.ToolCallContent); !ok {
		t.Errorf("message 2 kept %+v", got.Messages[2].Content)
	}
	if tc, ok := got.Messages[4].Content[0].(types.TextContent); !ok || tc.Text != "" || tc.ProviderOptions == nil {
		t.Errorf("message 4 lost the empty text part with provider options: %+v", got.Messages[4].Content)
	}
}

// TestR87genLegacyToolResultURLsDownloaded ports 933dae31f6
// "should download URL content in deprecated file-url and image-url tool
// results": both legacy URL items are planned for download in order and
// become inline data; the file-url keeps its declared full media type, the
// image-url takes the downloaded one.
func TestR87genLegacyToolResultURLsDownloaded(t *testing.T) {
	var output types.ToolResultOutput
	if err := json.Unmarshal([]byte(`{"type":"content","value":[`+
		`{"type":"file-url","url":"https://example.com/deprecated-file.pdf","mediaType":"application/pdf"},`+
		`{"type":"image-url","url":"https://example.com/deprecated-image.png"}]}`), &output); err != nil {
		t.Fatal(err)
	}
	var planned []DownloadRequest
	download := func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
		planned = append(planned, requests...)
		out := make([]*DownloadResult, len(requests))
		for i := range requests {
			out[i] = &DownloadResult{Data: []byte{1, 2, 3}, MediaType: "application/octet-stream"}
		}
		return out, nil
	}
	got, err := NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "toolCallId", ToolName: "toolName", Input: `{}`}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "toolCallId", ToolName: "toolName", Output: &output}}},
	}}, true, download, func(string, string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	wantPlan := []DownloadRequest{{URL: "https://example.com/deprecated-file.pdf"}, {URL: "https://example.com/deprecated-image.png"}}
	if !reflect.DeepEqual(planned, wantPlan) {
		t.Fatalf("planned downloads %+v, want %+v", planned, wantPlan)
	}
	var result types.ToolResultContent
	switch p := got.Messages[1].Content[0].(type) {
	case types.ToolResultContent:
		result = p
	case *types.ToolResultContent:
		result = *p
	default:
		t.Fatalf("tool message part %T", p)
	}
	wantMedia := []string{"application/pdf", "application/octet-stream"}
	for i, block := range result.Output.Content {
		fb, ok := block.(types.FileContentBlock)
		if !ok {
			t.Fatalf("block %d = %T", i, block)
		}
		if fb.FileData.Type != types.FileDataTypeData || !reflect.DeepEqual(fb.FileData.Data, []byte{1, 2, 3}) || fb.MediaType != wantMedia[i] {
			t.Errorf("block %d = %+v (media %q), want inline [1 2 3] %q", i, fb.FileData, fb.MediaType, wantMedia[i])
		}
	}
}
