package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r82provbPNG = "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%"

// TS convertToDeepSeekChatMessages: `data:${type};base64,${convertToBase64(
// part.data.data)}` keeps a base64 string as is; resolveFullMediaType
// sniffs only its prefix.
func TestR82provbDeepSeekImageDataStringSentAsIs(t *testing.T) {
	f := types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: r82provbPNG}}
	part, err := deepseekResolveUserImagePart(f, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if got := part["image_url"].(map[string]interface{})["url"]; got != "data:image/png;base64,"+r82provbPNG {
		t.Fatalf("user image url = %v", got)
	}
	part, err = deepseekResolveToolResultImagePart(f, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if got := part["image_url"].(map[string]interface{})["url"]; got != "data:image/png;base64,"+r82provbPNG {
		t.Fatalf("tool image url = %v", got)
	}
}
