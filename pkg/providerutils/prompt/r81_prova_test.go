package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r81provaAnthropicFile(mediaType, data string) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}},
	}}}
}

// TS convertToBase64 returns a string payload unchanged for image and PDF
// base64 sources, and convertBytesDataToString decodes it with
// convertBase64ToUint8Array for text/plain. Go decoded every string with
// base64.StdEncoding up front: a string that is not standard base64 failed the
// conversion and a base64url string was re-encoded.
func TestR81provaAnthropicDataStringPassThrough(t *testing.T) {
	for _, tc := range []struct{ mediaType, data, want string }{
		{"image/png", "-_8", `"data":"-_8"`},
		{"application/pdf", "base64PDFdata", `"data":"base64PDFdata"`},
		{"text/plain", "aGk", `"data":"hi"`},
	} {
		out, err := ConvertToAnthropicPrompt(r81provaAnthropicFile(tc.mediaType, tc.data), AnthropicPromptOptions{})
		if err != nil {
			t.Fatalf("%s: %v", tc.mediaType, err)
		}
		b, _ := json.Marshal(out.Messages)
		if !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s: messages %s, want %s", tc.mediaType, b, tc.want)
		}
	}
	// text/plain needs the bytes: atob throws for a string that is not base64.
	if _, err := ConvertToAnthropicPrompt(r81provaAnthropicFile("text/plain", "base64PDFdata"), AnthropicPromptOptions{}); err == nil {
		t.Fatal("text/plain with invalid base64: want an error")
	}
}
