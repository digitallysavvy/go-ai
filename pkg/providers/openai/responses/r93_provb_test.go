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

// TS: an approval response with an empty approvalId is still sent (the
// processed-id set only de-duplicates), and additionalTools' optional
// description / strict reject null.
func TestR93provbToolMessageEdges(t *testing.T) {
	msg := types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "", Approved: true}}}
	items, err := convertToolItems(msg, ConvertOptions{processedApprovalIDs: map[string]bool{}})
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %v, %v", items, err)
	}
	result := types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Result: "ok",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"additionalTools": []interface{}{
			map[string]interface{}{"type": "function", "name": "x", "parameters": map[string]interface{}{}, "description": nil},
		}}}}
	if _, err := convertToolItems(types.Message{Role: types.RoleTool, Content: []types.ContentPart{result}}, ConvertOptions{}); err == nil {
		t.Fatal("null description accepted")
	}
}

// A legacy ToolResultContent.Result map is sent as JSON (TS json output),
// not as Go %v text.
func TestR93provbLegacyResultIsJSON(t *testing.T) {
	got := toolResultOutput(types.ToolResultContent{ToolCallID: "c", ToolName: "t", Result: map[string]interface{}{"a": 1}})
	if got != `{"a":1}` {
		t.Fatalf("output = %#v", got)
	}
}
