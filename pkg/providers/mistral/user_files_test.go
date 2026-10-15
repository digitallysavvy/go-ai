package mistral

import (
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func userFile(fd types.FileData, mediaType string) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{MediaType: mediaType, FileData: fd}}}}
}

// TS convertToMistralChatMessages: images -> image_url, PDFs -> document_url,
// URL and data shapes both honored; everything else is unsupported.
func TestUserFileParts(t *testing.T) {
	out, err := ConvertToMistralChatMessagesWithError(userFile(types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2}}, "image/png"))
	if err != nil {
		t.Fatal(err)
	}
	part := out[0]["content"].([]map[string]interface{})[0]
	if part["type"] != "image_url" || !strings.HasPrefix(part["image_url"].(string), "data:image/png;base64,") {
		t.Fatalf("image part = %#v", part)
	}
	out, err = ConvertToMistralChatMessagesWithError(userFile(types.FileData{Type: types.FileDataTypeURL, URL: "https://x/a.pdf"}, "application/pdf"))
	if err != nil {
		t.Fatal(err)
	}
	part = out[0]["content"].([]map[string]interface{})[0]
	if part["type"] != "document_url" || part["document_url"] != "https://x/a.pdf" {
		t.Fatalf("pdf part = %#v", part)
	}
	for name, msgs := range map[string][]types.Message{
		"reference": userFile(types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"mistral": "f"}}, "application/pdf"),
		"text":      userFile(types.FileData{Type: types.FileDataTypeText, Text: "hi"}, "text/plain"),
		"audio":     userFile(types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, "audio/mp3"),
	} {
		if _, err := ConvertToMistralChatMessagesWithError(msgs); !providererrors.IsUnsupportedFunctionalityError(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, _, err := convertToMistralConversationInputs(msgs, newConversationToolNames(nil)); !providererrors.IsUnsupportedFunctionalityError(err) {
			t.Errorf("%s (conversation): err = %v", name, err)
		}
	}
}
