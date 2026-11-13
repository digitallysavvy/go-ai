package huggingface

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r82provbPNG = "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%"

// TS: `data:${resolveFullMediaType({ part })};base64,${part.data.data}`; the
// type is sniffed from the string's prefix with the image signatures.
func TestR82provbHuggingFaceImageDataStringSentAsIs(t *testing.T) {
	part, err := hfConvertFileToImagePart(types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "data:image/png;base64," + r82provbPNG; part.ImageURL != want {
		t.Fatalf("image url = %q, want %q", part.ImageURL, want)
	}
	// A TIFF header is an image signature TS detects (http sniffing does not).
	if _, err := hfConvertFileToImagePart(types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{0x49, 0x49, 0x2a, 0x00, 1, 2, 3, 4}}}); err != nil {
		t.Fatalf("tiff bytes: %v", err)
	}
}
