package openresponses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs reports the stopSequences/topK/seed warnings before the input
// conversion warnings.
func TestBuildRequestBody_CallSettingWarningsPrecedeInputWarnings(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:1234/v1"}), "gpt-5")
	topK := 3
	_, warnings, err := m.buildRequestBody(&provider.GenerateOptions{
		TopK: &topK,
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleTool,
			Content: []types.ContentPart{types.ToolResultContent{
				ToolCallID: "call_1",
				ToolName:   "read",
				Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{types.FileContentBlock{
						MediaType: "text/plain",
						FileData:  types.FileData{Type: types.FileDataTypeText, Text: "t"},
					}},
				},
			}},
		}}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || warnings[0].Feature != "topK" || warnings[1].Type != "other" {
		t.Fatalf("warnings = %#v, want topK first, then the input warning", warnings)
	}
}
