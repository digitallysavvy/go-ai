package responses

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var testPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}

func userImagePrompt(mediaType string, data []byte) types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: mediaType, Data: data},
	}}}}
}

// TS resolveFullMediaType: a bare or wildcard media type on inline bytes is
// sniffed from the bytes, and unidentifiable bytes are an
// UnsupportedFunctionalityError.
func TestOpenAIResponsesInlineBareImageMediaTypeIsSniffed(t *testing.T) {
	for _, mt := range []string{"image", "image/*"} {
		input, _, err := ConvertPromptToInputWithOptions(userImagePrompt(mt, testPNG), "", ConvertOptions{})
		if err != nil {
			t.Fatalf("%s: %v", mt, err)
		}
		part := input[0].(UserMessage).Content.([]interface{})[0].(UserImageURLPart)
		if part.ImageURL != "data:image/png;base64,iVBORw0KGgoAAAAA" {
			t.Fatalf("%s: image_url = %s", mt, part.ImageURL)
		}
	}

	_, _, err := ConvertPromptToInputWithOptions(userImagePrompt("image", []byte{1, 2, 3}), "", ConvertOptions{})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError for undetectable bytes", err)
	}
}

func TestXAIResponsesInlineBareImageMediaTypeIsSniffed(t *testing.T) {
	input, _, err := ConvertPromptToXAIInput(userImagePrompt("image", testPNG))
	if err != nil {
		t.Fatal(err)
	}
	part := input[0].(UserMessage).Content.([]interface{})[0].(UserImageURLPart)
	if part.ImageURL != "data:image/png;base64,iVBORw0KGgoAAAAA" {
		t.Fatalf("image_url = %s", part.ImageURL)
	}
}
