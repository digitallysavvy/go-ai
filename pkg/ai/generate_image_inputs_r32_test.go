package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS toImageModelV4File gives every input image a media type: a data URL's
// own, else detected from the bytes, else image/png.
func TestGenerateImageNormalizesInputImageMediaTypes(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	var got *provider.ImageGenerateOptions
	model := &testutil.MockImageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			got = opts
			return &types.ImageResult{Image: png}, nil
		},
	}
	_, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:  model,
		Prompt: "edit",
		Files: []provider.ImageFile{
			{Type: "file", Data: png},
			{Type: "file", Data: []byte("unknown"), MediaType: "image/webp"},
			{Type: "url", URL: "data:;base64,AQI="},
			{Type: "url", URL: "data:image/jpeg;base64,AQI="},
		},
		Mask: &provider.ImageFile{Type: "file", Data: []byte("zz")},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ typ, mt string }{{"file", "image/png"}, {"file", "image/webp"}, {"file", "image/png"}, {"file", "image/jpeg"}}
	for i, w := range want {
		if got.Files[i].Type != w.typ || got.Files[i].MediaType != w.mt {
			t.Errorf("file %d = %+v, want %v", i, got.Files[i], w)
		}
	}
	if got.Mask == nil || got.Mask.MediaType != "image/png" {
		t.Errorf("mask = %+v, want image/png default", got.Mask)
	}
}
