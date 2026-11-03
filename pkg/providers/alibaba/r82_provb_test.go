package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provbPNG is a base64 string whose 24-character prefix (TS decodePrefix
// for 18 bytes) is a PNG header and whose tail is not base64.
const r82provbPNG = "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%"

// TS formatImageUrl: convertToBase64 returns a base64 string as is and
// resolveFullMediaType sniffs only its prefix.
func TestR82provbAlibabaImageDataStringSentAsIs(t *testing.T) {
	url, err := alibabaImageURL(types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "data:image/png;base64," + r82provbPNG; url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}
