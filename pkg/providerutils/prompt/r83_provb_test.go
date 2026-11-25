package prompt

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r83provbTextDecoderInput is EF BB BF 'A' FF FE 'B' E0 80 'C' F0 9F 98; Node
// new TextDecoder().decode(...) returns "A��B��C�".
var r83provbTextDecoderInput = []byte{0xEF, 0xBB, 0xBF, 0x41, 0xFF, 0xFE, 0x42, 0xE0, 0x80, 0x43, 0xF0, 0x9F, 0x98}

const r83provbTextDecoderWant = "A��B��C�"

func TestR83provbDecodeUTF8TextMatchesTextDecoder(t *testing.T) {
	if got := DecodeUTF8Text(r83provbTextDecoderInput); got != r83provbTextDecoderWant {
		t.Fatalf("DecodeUTF8Text = %q, want %q", got, r83provbTextDecoderWant)
	}
	if got := DecodeUTF8Text([]byte("\xEF\xBB\xBFhi")); got != "hi" {
		t.Fatalf("BOM not dropped: %q", got)
	}
}

// TS convert-to-openai-compatible-chat-messages.ts 147-161: a text file part
// is new TextDecoder().decode(bytes) (BOM dropped, one U+FFFD per invalid
// subpart), for both byte and base64 string payloads.
func TestR83provbCompatTextPartUsesTextDecoder(t *testing.T) {
	parts := []types.FileContent{
		{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, Data: r83provbTextDecoderInput}},
		{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: base64.StdEncoding.EncodeToString(r83provbTextDecoderInput)}},
	}
	for i, part := range parts {
		msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{part, types.TextContent{Text: "x"}}}}
		out, err := ToOpenAIMessagesWithError(msgs, ToOpenAIMessagesOptions{FileParts: FilePartsOpenAICompatible})
		if err != nil {
			t.Fatal(err)
		}
		content := out[0]["content"].([]map[string]interface{})
		if got := content[0]["text"]; got != r83provbTextDecoderWant {
			t.Fatalf("part %d text = %q, want %q", i, got, r83provbTextDecoderWant)
		}
	}
}
