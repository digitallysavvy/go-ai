package prompt

import (
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFileDataErrorTypesAndParsing(t *testing.T) {
	inlineErr := &InvalidInlineDataURLError{Value: "data:bad", Cause: errors.New("bad")}
	if !strings.Contains(inlineErr.Error(), "data:bad") {
		t.Fatalf("InvalidInlineDataURLError.Error() = %q", inlineErr.Error())
	}
	if !errors.Is(inlineErr, inlineErr.Cause) {
		t.Fatal("InvalidInlineDataURLError should unwrap cause")
	}

	reasoningErr := &ReasoningFileConstraintError{Type: types.FileDataTypeReference}
	if !strings.Contains(reasoningErr.Error(), string(types.FileDataTypeReference)) {
		t.Fatalf("ReasoningFileConstraintError.Error() = %q", reasoningErr.Error())
	}
	if got := (&UnsupportedSystemMessageError{}).Error(); got == "" {
		t.Fatal("UnsupportedSystemMessageError.Error() should not be empty")
	}

	if _, err := ParseDataURL("https://example.com/nope"); err == nil {
		t.Fatal("ParseDataURL(non-data URL) should fail")
	}
	if _, err := ParseDataURL("data:text/plain;base64"); err == nil {
		t.Fatal("ParseDataURL(missing comma) should fail")
	}
	if _, err := ParseDataURL("data:text/plain,abc"); err == nil {
		t.Fatal("ParseDataURL(non-base64 marker) should fail")
	}
}

func TestNormalizeContentPartsAndToolResultPassThrough(t *testing.T) {
	parts, err := NormalizeContentParts(nil)
	if err != nil || parts != nil {
		t.Fatalf("NormalizeContentParts(nil) = (%#v, %v)", parts, err)
	}

	normalized, err := NormalizeContentParts([]types.ContentPart{
		&types.ImageContent{Image: []byte{1}, MimeType: "image/png"},
		&types.FileContent{Text: "t", MediaType: "text/plain"},
		&types.ReasoningFileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/r.txt", MediaType: "text/plain"}},
	})
	if err != nil {
		t.Fatalf("NormalizeContentParts(pointer variants) error = %v", err)
	}
	if _, ok := normalized[0].(types.FileContent); !ok {
		t.Fatalf("normalized[0] type = %T, want FileContent", normalized[0])
	}

	part := types.ToolResultContent{
		ToolCallID: "call-1",
		Output: &types.ToolResultOutput{
			Type: types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{
				&types.ImageContentBlock{Data: []byte{0x01}, MediaType: "image/png"},
			},
		},
	}
	toolResult, err := NormalizeToolResultContent(part)
	if err != nil {
		t.Fatalf("NormalizeToolResultContent() error = %v", err)
	}
	if _, ok := toolResult.Output.Content[0].(types.FileContentBlock); !ok {
		t.Fatalf("toolResult output block type = %T, want FileContentBlock", toolResult.Output.Content[0])
	}

	unchanged, err := NormalizeToolResultContent(types.ToolResultContent{
		ToolCallID: "call-2",
		Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"},
	})
	if err != nil {
		t.Fatalf("NormalizeToolResultContent(text output) error = %v", err)
	}
	if unchanged.Output.Type != types.ToolResultOutputText {
		t.Fatalf("NormalizeToolResultContent(text output) changed type = %v", unchanged.Output.Type)
	}
}
