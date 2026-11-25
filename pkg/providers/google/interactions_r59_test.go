package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r59: a legacy ImageContentBlock in a tool result is TS's file item with
// inline data (filePartToImageBlock): a bare "image" type is completed by
// sniffing the bytes, and a non-image type is a document block now that the
// converter handles every file kind (TS afb4e69924).
func TestR59InteractionsToolResultImageBlockResolvesMediaType(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	blocks, warnings, err := toolResultContentBlocks([]types.ToolResultContentBlock{
		types.ImageContentBlock{Data: png, MediaType: "image"},
		types.ImageContentBlock{Data: png},
		types.ImageContentBlock{Data: []byte("%PDF"), MediaType: "application/pdf"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("blocks = %v, want 3", blocks)
	}
	for i, b := range blocks[:2] {
		if b["type"] != "image" || b["mime_type"] != "image/png" {
			t.Errorf("block %d = %v, want image with mime_type image/png", i, b)
		}
	}
	if blocks[2]["type"] != "document" || blocks[2]["mime_type"] != "application/pdf" {
		t.Errorf("block 2 = %v, want a pdf document", blocks[2])
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
}
