package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r82provbPNG = "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%"

// TS formatMediaUrl sends a base64 string as is (sniffed from its prefix);
// a text part decodes the string (convertBase64ToUint8Array, unpadded ok).
func TestR82provbMoonshotDataStringParts(t *testing.T) {
	part, err := convertMoonshotFilePart(types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if got := part["image_url"].(map[string]interface{})["url"]; got != "data:image/png;base64,"+r82provbPNG {
		t.Fatalf("image url = %v", got)
	}
	part, err = convertMoonshotFilePart(types.FileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "aGVsbG8"}})
	if err != nil {
		t.Fatal(err)
	}
	if part["text"] != "hello" {
		t.Fatalf("text part = %v, want hello", part)
	}
}
