package cohere

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS buildImageUrl embeds convertToBase64(part.data.data), which returns a
// base64 string payload as is; Go read only the bytes, so a DataString image
// became an empty data URL (and a bare image type could not be sniffed).
func TestR81provaCohereImageDataStringPayload(t *testing.T) {
	for _, tc := range []struct{ mediaType, data, want string }{
		{"image/png", "-_8", "data:image/png;base64,-_8"},
		{"image", "iVBORw", "data:image/png;base64,iVBORw"},
	} {
		got, err := fileDataToImageURL(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, MediaType: tc.mediaType, DataString: tc.data}})
		if err != nil || got != tc.want {
			t.Fatalf("%s: url = %q, %v, want %q", tc.mediaType, got, err, tc.want)
		}
	}
}
