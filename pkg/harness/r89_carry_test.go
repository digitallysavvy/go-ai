package harness

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS utils/normalize-harness-tool-model-output.ts: the error names the
// part type and, for a file, its data type; a media type without a full
// subtype ("image/") is detected. Messages recorded from TS 382d0b8eaf
// (bun) for the same parts.
func TestR89carryNormalizeToolModelOutputErrors(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	const prefix = "Harnesses support only text and inline images in tool model output. "
	cases := []struct {
		name  string
		block types.ToolResultContentBlock
		want  string
	}{
		{"file url", types.FileContentBlock{MediaType: "image/png", URL: "https://x/a.png"}, prefix + "Unsupported content 'file' (image/png, url)."},
		{"file reference", types.FileContentBlock{MediaType: "image/png", Reference: "file_1"}, prefix + "Unsupported content 'file' (image/png, reference)."},
		{"file text", types.FileContentBlock{MediaType: "image/png", Text: "x"}, prefix + "Unsupported content 'file' (image/png, text)."},
		{"image data not an image", types.ImageContentBlock{MediaType: "text/plain", Data: png}, prefix + "Unsupported content 'image-data' (text/plain, data)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{tc.block}})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %s", err, tc.want)
			}
		})
	}
	t.Run("image/ is detected", func(t *testing.T) {
		out, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image/", Data: png},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if img, ok := out.Content[0].(types.ImageContentBlock); !ok || img.MediaType != "image/png" {
			t.Fatalf("got %#v", out.Content[0])
		}
	})
}
