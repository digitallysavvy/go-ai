package prompt

import (
	"errors"
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

// TS convertToLanguageModelV4FilePart rejects a data: string in the data
// variant with "Data URLs are not valid inline data. Pass them as { type:
// "url", url } instead." (file-part-data.ts 95-103).
func TestR90genDataVariantDataURLString(t *testing.T) {
	_, err := NormalizeFileContent(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "data:text/plain;base64,aGk=", MediaType: "text/plain"}})
	var invalid *InvalidInlineDataURLError
	if !errors.As(err, &invalid) || invalid.Cause == nil || invalid.Cause.Error() != `Data URLs are not valid inline data. Pass them as { type: "url", url } instead.` {
		t.Fatalf("err = %v", err)
	}
}
