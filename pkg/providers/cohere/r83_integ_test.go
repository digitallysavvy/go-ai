package cohere

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR83integCohereDocumentTextUsesTextDecoder: TS
// convert-to-cohere-chat-prompt.ts 94-97 decodes byte data with new
// TextDecoder().decode (BOM dropped, U+FFFD per maximal invalid subpart); a
// string payload stays as given.
func TestR83integCohereDocumentTextUsesTextDecoder(t *testing.T) {
	in := []byte{0xEF, 0xBB, 0xBF, 0x41, 0xFF, 0xFE, 0x42, 0xE0, 0x80, 0x43, 0xF0, 0x9F, 0x98}
	got, err := cohereDocumentText(types.FileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, Data: in}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "A\uFFFD\uFFFDB\uFFFD\uFFFDC\uFFFD"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	got, _ = cohereDocumentText(types.FileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "\uFEFFraw"}})
	if got != "\uFEFFraw" {
		t.Fatalf("string payload = %q", got)
	}
}
