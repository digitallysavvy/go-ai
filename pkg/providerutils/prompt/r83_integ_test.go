package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r83integTextDecoderInput is EF BB BF 41 FF FE 42 E0 80 43 F0 9F 98; Node
// new TextDecoder().decode gives "A\uFFFD\uFFFDB\uFFFD\uFFFDC\uFFFD".
var r83integTextDecoderInput = []byte{0xEF, 0xBB, 0xBF, 0x41, 0xFF, 0xFE, 0x42, 0xE0, 0x80, 0x43, 0xF0, 0x9F, 0x98}

const r83integTextDecoderWant = "A\uFFFD\uFFFDB\uFFFD\uFFFDC\uFFFD"

// TestR83integAnthropicTextDocumentUsesTextDecoder: a text/plain file part
// becomes a text document whose data is TS convertBytesDataToString (new
// TextDecoder().decode): BOM dropped, one U+FFFD per maximal invalid subpart.
func TestR83integAnthropicTextDocumentUsesTextDecoder(t *testing.T) {
	out, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, Data: r83integTextDecoderInput}},
	}}}, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Messages)
	want, _ := json.Marshal(r83integTextDecoderWant)
	if !strings.Contains(string(b), `"data":`+string(want)) {
		t.Fatalf("messages = %s, want data %s", b, want)
	}
}
