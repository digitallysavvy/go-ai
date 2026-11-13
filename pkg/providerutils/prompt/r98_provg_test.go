package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS googleVideoMetadataSchema.parse runs on the caller's own object (no
// secureJsonParse): a __proto__ / constructor.prototype key is an unknown
// key that zod strips, not a parse failure (cr-r98-tools-to-provg,
// cr-r98-prova-to-provg).
func TestR98provgGoogleVideoMetadataPrototypeKeysStripped(t *testing.T) {
	meta := map[string]interface{}{"fps": 2, "__proto__": map[string]interface{}{"x": 1}, "constructor": map[string]interface{}{"prototype": map[string]interface{}{}}}
	file := types.FileContent{MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/v.mp4"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"videoMetadata": meta}}}
	gp, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{file}}}, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(gp.Contents)
	if !strings.Contains(string(b), `"videoMetadata":{"fps":2}`) {
		t.Fatalf("contents = %s", b)
	}
}

// TS codeExecutionOutputSchema.parse(output.value) on the prompt's own value.
func TestR98provgGoogleCodeExecutionOutputPrototypeKeyStripped(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "c1", ToolName: "code_execution", ProviderExecuted: true, Input: `{"language":"PYTHON","code":"print(1)"}`},
		types.ToolResultContent{ToolCallID: "c1", ToolName: "code_execution", ProviderExecuted: true,
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"outcome": "OUTCOME_OK", "output": "1\n", "__proto__": map[string]interface{}{}}}},
	}}}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IsGemini3Model: true, IncludeFunctionCallIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"executableCode":{"code":"print(1)","language":"PYTHON"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1\n"}}]`
	if got := googleJSON(t, out.Contents[0]["parts"]); got != want {
		t.Fatalf("parts = %s", got)
	}
}
