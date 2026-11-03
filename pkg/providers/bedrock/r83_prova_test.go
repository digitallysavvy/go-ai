package bedrock

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS resolveFullMediaType throws for an s3 url part whose media type has no
// subtype ("... since it is not passed as inline bytes"), and
// convertToolResultOutput rejects a text file part in tool result content
// (`tool result file data of type "text"`).
func TestR83provaURLMediaTypeAndTextToolResultErrors(t *testing.T) {
	toolResult := func(block types.FileContentBlock) []types.Message {
		return []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "t",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{block}},
		}}}}
	}
	cases := []struct {
		name string
		msgs []types.Message
		want string
	}{
		{"user s3 bare image", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "s3://bucket/a.png"}},
		}}}, `'file of media type "image" must specify subtype since it is not passed as inline bytes' functionality not supported.`},
		{"user s3 wildcard video", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: "video/*", URL: "s3://bucket/a.mp4"},
		}}}, `'file of media type "video/*" must specify subtype since it is not passed as inline bytes' functionality not supported.`},
		{"tool result s3 bare image", toolResult(types.FileContentBlock{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "s3://bucket/a.png"}}),
			`'file of media type "image" must specify subtype since it is not passed as inline bytes' functionality not supported.`},
		{"tool result text file", toolResult(types.FileContentBlock{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeText, Text: "hi"}}),
			`'tool result file data of type "text"' functionality not supported.`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := convertToBedrockChatMessages(c.msgs, false)
			var uf *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &uf) {
				t.Fatalf("err = %T %v, want *UnsupportedFunctionalityError", err, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("message = %q, want %q", err.Error(), c.want)
			}
		})
	}
	// A full media type on an s3 url still converts.
	_, out, err := convertToBedrockChatMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "image/png", URL: "s3://bucket/a.png"},
	}}}, false)
	if err != nil || len(out) != 1 {
		t.Fatalf("s3 image/png: out=%v err=%v", out, err)
	}
}
