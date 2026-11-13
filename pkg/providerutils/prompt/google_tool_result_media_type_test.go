package prompt

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS appendToolResultParts resolves a top-level media type from the bytes.
func TestGoogleToolResultFileTopLevelMediaTypeIsSniffed(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c", ToolName: "t", Output: &types.ToolResultOutput{
			Type:    types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{types.FileContentBlock{Data: png, MediaType: "image"}},
		}},
	}}}
	got, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{SupportsFunctionResponseParts: true})
	if err != nil {
		t.Fatal(err)
	}
	fr := got.Contents[0]["parts"].([]map[string]interface{})[0]["functionResponse"].(map[string]interface{})
	inline := fr["parts"].([]map[string]interface{})[0]["inlineData"].(map[string]interface{})
	if inline["mimeType"] != "image/png" {
		t.Fatalf("mimeType = %v", inline["mimeType"])
	}
}

// TS resolveFullMediaType throws UnsupportedFunctionalityError when a
// top-level media type cannot be completed from the bytes; the conversion
// must fail instead of sending an incomplete mimeType.
func TestGoogleToolResultFileUndetectableTopLevelMediaTypeFails(t *testing.T) {
	for _, supportsParts := range []bool{true, false} {
		msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c", ToolName: "t", Output: &types.ToolResultOutput{
				Type:    types.ToolResultOutputContent,
				Content: []types.ToolResultContentBlock{types.FileContentBlock{Data: []byte("not an image"), MediaType: "image"}},
			}},
		}}}
		_, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{SupportsFunctionResponseParts: supportsParts})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) {
			t.Fatalf("supportsParts=%v: err = %v, want UnsupportedFunctionalityError", supportsParts, err)
		}
	}
}
