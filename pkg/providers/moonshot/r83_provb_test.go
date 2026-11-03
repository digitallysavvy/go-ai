package moonshot

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convert-to-moonshotai-chat-messages.ts 260-268: a text file part is
// new TextDecoder().decode(bytes): BOM dropped, one U+FFFD per invalid subpart.
func TestR83provbMoonshotTextPartUsesTextDecoder(t *testing.T) {
	raw := []byte{0xEF, 0xBB, 0xBF, 0x41, 0xFF, 0xFE, 0x42, 0xE0, 0x80, 0x43}
	want := "A��B��C"
	for _, fd := range []types.FileData{
		{Type: types.FileDataTypeData, Data: raw},
		{Type: types.FileDataTypeData, DataString: base64.StdEncoding.EncodeToString(raw)},
	} {
		part, err := convertMoonshotFilePart(types.FileContent{MediaType: "text/plain", FileData: fd})
		if err != nil {
			t.Fatal(err)
		}
		if part["text"] != want {
			t.Fatalf("text = %q, want %q", part["text"], want)
		}
	}
}
