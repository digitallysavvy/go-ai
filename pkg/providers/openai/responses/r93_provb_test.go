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

// TS parses a reasoning part's providerOptions with
// openaiResponsesReasoningProviderOptionsSchema: a non-string itemId fails.
func TestR93provbReasoningPartOptionsValidated(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": 5}}},
	}}}}
	if _, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{}); err == nil {
		t.Fatal("numeric itemId accepted")
	}
	prompt.Messages[0].Content[0] = types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_1", "reasoningEncryptedContent": nil}}}
	if _, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{Store: true}); err != nil {
		t.Fatal(err)
	}
}
