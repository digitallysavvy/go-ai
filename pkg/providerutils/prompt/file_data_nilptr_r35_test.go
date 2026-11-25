package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A nil part pointer in message content must not panic the prompt conversion.
func TestNormalizeContentParts_NilPointersDoNotPanic(t *testing.T) {
	var (
		img  *types.ImageContent
		file *types.FileContent
		rf   *types.ReasoningFileContent
		tr   *types.ToolResultContent
	)
	got, err := NormalizeContentParts([]types.ContentPart{img, file, rf, tr, types.TextContent{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d parts, want only the text part", len(got))
	}
	var fb *types.FileContentBlock
	var ib *types.ImageContentBlock
	out, err := NormalizeToolResultContent(types.ToolResultContent{Output: &types.ToolResultOutput{
		Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{fb, ib},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Output.Content) != 0 {
		t.Fatalf("content = %v", out.Output.Content)
	}
}
