package azure

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provbMaiImageURLType: a URL image is typed (and named) by the
// downloaded content type, as TS downloadBlob types the Blob
// (azure-mai-image-model.ts fileToBlob, toFormData).
func TestR90provbMaiImageURLType(t *testing.T) {
	server, c := newMaiServer(t, nil)
	_, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p",
		Files:  []provider.ImageFile{{Type: "url", URL: "data:image/jpeg;base64,AQI=", MediaType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.files, []string{"image-1.jpg"}) || !reflect.DeepEqual(c.fileType, []string{"image/jpeg"}) {
		t.Errorf("files = %v types = %v", c.files, c.fileType)
	}
}
