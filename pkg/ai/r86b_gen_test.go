package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// V061 item 3: GenerateText logs the TS mapToolResultOutput deprecation for
// a legacy tool-result content item on every step that sends it (without
// provider or model, as TS logWarnings({ warnings })).
func TestR86bgenGenerateTextLogsLegacyToolResultDeprecation(t *testing.T) {
	var settings []string
	SetLogWarnings(func(o LogWarningsOptions) {
		if o.Provider != "" || o.Model != "" {
			return
		}
		for _, w := range o.Warnings {
			settings = append(settings, w.Setting)
		}
	})
	t.Cleanup(func() { SetLogWarnings(nil) })

	var output types.ToolResultOutput
	if err := json.Unmarshal([]byte(`{"type":"content","value":[{"type":"file-data","data":"aGVsbG8=","mediaType":"text/plain"}]}`), &output); err != nil {
		t.Fatal(err)
	}
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "c2", ToolName: "t", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: "{}"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &output}}},
		},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	const setting = `"tool-result" content of type "file-data"`
	if len(settings) != 2 || settings[0] != setting || settings[1] != setting {
		t.Fatalf("logged settings = %q, want the file-data deprecation on both steps", settings)
	}
}
