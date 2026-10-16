package moonshot

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMoonshotInlineWildcardImageIsSniffed(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	out, _, err := convertToMoonshotChatMessages("kimi-k2.5", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "a"},
		types.FileContent{MediaType: "image/*", Data: png},
	}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	parts := out[0].(map[string]interface{})["content"].([]interface{})
	img := parts[1].(map[string]interface{})["image_url"].(map[string]interface{})["url"].(string)
	if !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("url = %s, want sniffed image/png data URL", img)
	}
}
