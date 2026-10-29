package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r59: a legacy ImageContentBlock in a tool result is TS's file item with
// inline data (filePartToImageBlock): a bare "image" type is completed by
// sniffing the bytes, and a non-image type is dropped with TS's warning.
func TestR59InteractionsToolResultImageBlockResolvesMediaType(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	blocks, warnings, err := toolResultContentBlocks([]types.ToolResultContentBlock{
		types.ImageContentBlock{Data: png, MediaType: "image"},
		types.ImageContentBlock{Data: png},
		types.ImageContentBlock{Data: []byte("%PDF"), MediaType: "application/pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %v, want 2", blocks)
	}
	for i, b := range blocks {
		if b["type"] != "image" || b["mime_type"] != "image/png" {
			t.Errorf("block %d = %v, want image with mime_type image/png", i, b)
		}
	}
	want := "google.interactions: tool-result file with mediaType \"application/pdf\" is not supported (Interactions `function_result.result` accepts only text and image content); part dropped."
	if len(warnings) != 1 || warnings[0].Message != want {
		t.Fatalf("warnings = %+v, want %q", warnings, want)
	}
}
