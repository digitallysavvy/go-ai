package bedrock

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS throws UnsupportedFunctionalityError (with its message) from the message
// converter; callers match the type with errors.As.
func TestConverterErrorsAreUnsupportedFunctionality(t *testing.T) {
	file := func(media string, data []byte) []types.Message {
		return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{MediaType: media, Data: data}}}}
	}
	cases := []struct {
		name string
		msgs []types.Message
		want string
	}{
		{"image mime type", file("image/bmp", []byte{1}), "Unsupported image mime type: image/bmp, expected one of: image/jpeg, image/png, image/gif, image/webp"},
		{"file mime type", file("application/zip", []byte{1}), "Unsupported file mime type: application/zip, expected one of: application/pdf, text/csv, application/msword, application/vnd.openxmlformats-officedocument.wordprocessingml.document, application/vnd.ms-excel, application/vnd.openxmlformats-officedocument.spreadsheetml.sheet, text/html, text/plain, text/markdown"},
		{"video mime type", file("video/avi", []byte{1}), "Unsupported video mime type: video/avi, expected one of: video/x-matroska, video/quicktime, video/mp4, video/webm, video/x-flv, video/mpeg, video/mpg, video/wmv, video/x-ms-wmv, video/3gpp"},
		{"undetectable", file("video", []byte{1, 2, 3}), `file of media type "video" must specify subtype since it could not be auto-detected`},
		{"system after user", []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
		}, "'Multiple system messages that are separated by user/assistant messages' functionality not supported."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := convertToBedrockChatMessages(c.msgs, false)
			var uf *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &uf) {
				t.Fatalf("err = %T %v, want *UnsupportedFunctionalityError", err, err)
			}
			if got := err.Error(); got != c.want && !contains(got, c.want) {
				t.Fatalf("message = %q, want %q", got, c.want)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
