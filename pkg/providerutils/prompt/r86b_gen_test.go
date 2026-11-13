package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r86bgenLegacyValue is the tool-result content of the TS run below.
const r86bgenLegacyValue = `{"type":"content","value":[` +
	`{"type":"text","text":"x"},` +
	`{"type":"file-data","data":"aGVsbG8=","mediaType":"text/plain","filename":"a.txt"},` +
	`{"type":"file-url","url":"https://example.com/report.pdf"},` +
	`{"type":"file-url","url":"https://example.com/noext"},` +
	`{"type":"file-url","url":"https://example.com/x.png","mediaType":""},` +
	`{"type":"file-url","url":"https://example.com/y","mediaType":"text/csv"},` +
	`{"type":"file-id","fileId":{"openai":"f1"},"mediaType":"application/pdf"},` +
	`{"type":"file-reference","providerReference":{"openai":"f2"}},` +
	`{"type":"image-data","data":"cG5n","mediaType":"image/png","filename":"i.png"},` +
	`{"type":"image-url","url":"https://example.com/i.png","mediaType":"image/png"},` +
	`{"type":"image-file-id","fileId":{"openai":"f3"}},` +
	`{"type":"image-file-reference","providerReference":{"openai":"f4"}}]}`

// r86bgenTSLegacyWarnings is TS convertToLanguageModelPrompt at 30548ee7f9
// (bun, AI_SDK_LOG_WARNINGS capturing [setting, message]) on a tool message
// whose tool-result content is r86bgenLegacyValue: one logWarnings batch.
const r86bgenTSLegacyWarnings = `[["\"tool-result\" content of type \"file-data\"","The \"file-data\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'data', data } instead."],["\"tool-result\" content of type \"file-url\"","The \"file-url\" tool result content part with URL \"https://example.com/report.pdf\" is missing a \"mediaType\". Inferred media type 'application/pdf' from URL. The \"file-url\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'url', url } instead."],["\"tool-result\" content of type \"file-url\"","The \"file-url\" tool result content part with URL \"https://example.com/noext\" is missing a \"mediaType\". Unable to infer media type from URL. Defaulting to 'application/octet-stream'. The \"file-url\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'url', url } instead."],["\"tool-result\" content of type \"file-url\"","The \"file-url\" tool result content part with URL \"https://example.com/x.png\" is missing a \"mediaType\". Inferred media type '' from URL. The \"file-url\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'url', url } instead."],["\"tool-result\" content of type \"file-url\"","The \"file-url\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'url', url } instead."],["\"tool-result\" content of type \"file-id\"","The \"file-id\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'reference', reference } instead."],["\"tool-result\" content of type \"file-reference\"","The \"file-reference\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'reference', reference } instead."],["\"tool-result\" content of type \"image-data\"","The \"image-data\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'data', data } instead."],["\"tool-result\" content of type \"image-url\"","The \"image-url\" type for tool result content is deprecated. Use the \"file\" type with mediaType 'image' (or a specific image/* subtype) and { type: 'url', url } instead."],["\"tool-result\" content of type \"image-file-id\"","The \"image-file-id\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'reference', reference } instead."],["\"tool-result\" content of type \"image-file-reference\"","The \"image-file-reference\" type for tool result content is deprecated. Use the \"file\" type with mediaType and { type: 'reference', reference } instead."]]`

// V061 item 3: TS mapToolResultOutput logs a `deprecated` warning for each
// legacy tool-result content item (in tool messages and in assistant
// messages' provider-executed tool results); Go decoded them into
// FileContentBlock and logged none.
func TestR86bgenLegacyToolResultDeprecations(t *testing.T) {
	var output types.ToolResultOutput
	if err := json.Unmarshal([]byte(r86bgenLegacyValue), &output); err != nil {
		t.Fatal(err)
	}
	var want [][2]string
	if err := json.Unmarshal([]byte(r86bgenTSLegacyWarnings), &want); err != nil {
		t.Fatal(err)
	}
	pairs := func(warnings []types.Warning) [][2]string {
		var got [][2]string
		for _, w := range warnings {
			if w.Type != "deprecated" {
				t.Errorf("warning type = %q", w.Type)
			}
			got = append(got, [2]string{w.Setting, w.Message})
		}
		return got
	}

	toolMessage := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &output},
	}}
	if got := pairs(DeprecatedContentWarnings(toolMessage)); !reflect.DeepEqual(got, want) {
		t.Fatalf("tool message warnings =\n%q\nwant\n%q", got, want)
	}

	// TS: a provider-executed tool result in an assistant message logs too.
	var imageURL types.ToolResultOutput
	if err := json.Unmarshal([]byte(`{"type":"content","value":[{"type":"image-url","url":"https://example.com/a.png"}]}`), &imageURL); err != nil {
		t.Fatal(err)
	}
	assistant := types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "c0", ToolName: "t", ProviderExecuted: true},
		&types.ToolResultContent{ToolCallID: "c0", ToolName: "t", Output: &imageURL},
	}}
	if got := pairs(DeprecatedContentWarnings(assistant)); len(got) != 1 || got[0] != want[8] {
		t.Fatalf("assistant warnings = %q, want [%q]", got, want[8])
	}

	// A "file" block (the current type) logs nothing, nor does a user
	// message's tool result content.
	var current types.ToolResultOutput
	if err := json.Unmarshal([]byte(`{"type":"content","value":[{"type":"file","data":{"type":"url","url":"https://example.com/a.png"},"mediaType":"image/png"}]}`), &current); err != nil {
		t.Fatal(err)
	}
	if got := DeprecatedContentWarnings(types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", Output: &current}}}); len(got) != 0 {
		t.Fatalf("file block warnings = %v", got)
	}
}

// TS mapToolResultOutput result shapes for the legacy items (same TS run:
// [mediaType, filename] per item): only "file-data" keeps filename, and
// "file-id" / "file-reference" / "image-url" / "image-file-*" ignore an
// item mediaType; "file-url" keeps mediaType "" (TS `??`).
func TestR86bgenLegacyToolResultShapes(t *testing.T) {
	var output types.ToolResultOutput
	if err := json.Unmarshal([]byte(r86bgenLegacyValue), &output); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{
		{"text/plain", "a.txt"}, {"application/pdf", ""}, {"application/octet-stream", ""}, {"", ""}, {"text/csv", ""},
		{"application", ""}, {"application", ""}, {"image/png", ""}, {"image", ""}, {"image", ""}, {"image", ""},
	}
	for i, w := range want {
		block, ok := output.Content[i+1].(types.FileContentBlock)
		if !ok {
			t.Fatalf("block %d = %T", i+1, output.Content[i+1])
		}
		if block.MediaType != w[0] || block.Filename != w[1] {
			t.Errorf("block %d = mediaType %q filename %q, want %q %q", i+1, block.MediaType, block.Filename, w[0], w[1])
		}
	}
}
