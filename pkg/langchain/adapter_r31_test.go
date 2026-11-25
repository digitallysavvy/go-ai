package langchain

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A URL object whose protocol is not data: is a url block whatever its scheme
// (TS `data instanceof URL`); only a bare string falls to the base64 branch.
func TestConvertUserContentNonWebURLIsAURLBlock(t *testing.T) {
	got := convertUserContent([]types.ContentPart{
		types.ImageContent{URL: "gs://bucket/pic.png", MimeType: "image/png"},
		types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "s3://bucket/doc.pdf"}, MediaType: "application/pdf", Filename: "doc.pdf"},
		types.FileContent{URL: "gs://bucket/img.png", MediaType: "image/png"},
	})
	want := []interface{}{
		map[string]interface{}{"type": "image", "url": "gs://bucket/pic.png"},
		map[string]interface{}{"type": "file", "url": "s3://bucket/doc.pdf", "mimeType": "application/pdf", "filename": "doc.pdf"},
		map[string]interface{}{"type": "image", "url": "gs://bucket/img.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
