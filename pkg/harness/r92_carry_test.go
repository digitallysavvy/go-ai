package harness

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS normalizeHarnessToolModelOutput: an image-data part becomes
// `{...part, type: 'file', mediaType, data: {type: 'data', data}}`.
func TestR92carryNormalizeToolModelOutputImageDataBecomesFile(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	opts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}
	out, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.ImageContentBlock{Data: png, MediaType: "image", ProviderOptions: opts},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f, ok := out.Content[0].(types.FileContentBlock)
	if !ok || f.MediaType != "image/png" || f.FileData.Type != types.FileDataTypeData || string(f.FileData.Data) != string(png) || f.ProviderOptions["anthropic"] == nil {
		t.Fatalf("normalized = %#v", out.Content[0])
	}
	b, err := json.Marshal(out.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"file","data":{"type":"data","data":"iVBORw0KGgo="},"mediaType":"image/png","providerOptions":{"anthropic":{"cacheControl":{"type":"ephemeral"}}}}`; string(b) != want {
		t.Fatalf("json = %s\nwant   %s", b, want)
	}
	parts, isError, err := ConvertToolModelOutput(out)
	if err != nil || isError || len(parts) != 1 || parts[0].Type != ToolModelOutputContentImage || parts[0].MediaType != "image/png" {
		t.Fatalf("converted = %+v, %v, %v", parts, isError, err)
	}
}
