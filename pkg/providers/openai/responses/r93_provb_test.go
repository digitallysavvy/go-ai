package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS converts inline file bytes as given: an empty image is sent as an
// empty base64 data URL instead of being dropped (OpenAI and xAI inputs).
func TestR93provbEmptyInlineFileIsSent(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{}}},
	}}}}
	input, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	if !strings.Contains(string(b), `"image_url":"data:image/png;base64,"`) {
		t.Fatalf("openai input = %s", b)
	}
	xinput, _, err := ConvertPromptToXAIInput(prompt)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(xinput)
	if !strings.Contains(string(b), `"image_url":"data:image/png;base64,"`) {
		t.Fatalf("xai input = %s", b)
	}
}
