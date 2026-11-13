package openresponses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provbImageFullMediaType (cr-r95-tools-to-provb): TS
// convert-to-open-responses-input uses resolveFullMediaType for an inline
// image, so "image/" or "image/*" on PNG bytes is sent as image/png.
func TestR95provbImageFullMediaType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	for _, mt := range []string{"image", "image/", "image/*"} {
		var warnings []types.Warning
		out, err := convertUserContent([]types.ContentPart{types.FileContent{MediaType: mt, Data: png}}, &warnings, "openResponses")
		if err != nil {
			t.Fatalf("%q: %v", mt, err)
		}
		img, ok := out[0].(InputImageContent)
		if !ok || !strings.HasPrefix(img.ImageURL, "data:image/png;base64,") {
			t.Errorf("%q: part = %#v, want a data:image/png input image", mt, out[0])
		}
	}
}
