package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToLanguageModelPrompt passes a file part's media type on as
// given: a TS 9f7a842ae1 run sends "application/x; y", "text/",
// "a/b;;c=d" and `text/plain; charset="x` to the model unchanged for data,
// url, text and reference file data.
func TestR90genFileMediaTypeNotValidated(t *testing.T) {
	for _, mediaType := range []string{"application/x; y", "text/", "a/b;;c=d", `text/plain; charset="x`} {
		for _, data := range []types.FileData{
			{Type: types.FileDataTypeData, Data: []byte("hello")},
			{Type: types.FileDataTypeURL, URL: "https://example.com/f"},
			{Type: types.FileDataTypeText, Text: "hi"},
			{Type: types.FileDataTypeReference, Reference: map[string]string{"openai": "file-1"}},
		} {
			data.MediaType = mediaType
			got, err := NormalizeFileContent(types.FileContent{FileData: data, MediaType: mediaType})
			if err != nil {
				t.Fatalf("%s %q: %v", data.Type, mediaType, err)
			}
			if got.MediaType != mediaType || got.FileData.MediaType != mediaType {
				t.Fatalf("%s %q: media type %q / %q", data.Type, mediaType, got.MediaType, got.FileData.MediaType)
			}
		}
	}
}
