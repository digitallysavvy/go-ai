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

// V061 item 29: a model tool call has no title or toolMetadata
// (LanguageModelV4ToolCall); TS parseToolCall's invalid call takes both from
// the tool only (`title: tool?.title`). Valid calls keep the v0.5 Go
// behavior (a ToolMetadata a custom provider sets is kept when the tool has
// none, TestToolLoopAgentPreservesToolMetadataOnResults), so only the
// invalid cases are compared.
// Expected: TS parseToolCall at 30548ee7f9 under bun on calls carrying an
// extra title "P" and toolMetadata {p: 1}:
// {"unknown":{"title":null,"toolMetadata":null,"invalid":true},
// "known":{"title":null,"toolMetadata":null,"invalid":false},
// "knownInvalid":{"title":null,"toolMetadata":null,"invalid":true},
// "metaInvalid":{"title":"T","toolMetadata":{"m":1},"invalid":true},
// "providerDynamic":{"title":null,"toolMetadata":null,"invalid":false}}.
func TestR86bgenParseToolCallTitleMetadataFromToolOnly(t *testing.T) {
	tools := []types.Tool{
		{Name: "known", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "meta", Title: "T", Metadata: map[string]interface{}{"m": 1}, Parameters: map[string]interface{}{"type": "object"}},
	}
	call := func(id, name, input string) types.ToolCall {
		return types.ToolCall{ID: id, ToolName: name, RawArguments: input, Title: "P", ToolMetadata: map[string]interface{}{"p": 1}}
	}
	cases := []struct {
		name     string
		tools    []types.Tool
		call     types.ToolCall
		title    string
		metadata map[string]interface{}
		invalid  bool
	}{
		{"unknown", tools, call("1", "nope", "{}"), "", nil, true},
		{"knownInvalid", tools, call("3", "known", "{"), "", nil, true},
		{"metaInvalid", tools, call("4", "meta", "{"), "T", map[string]interface{}{"m": 1}, true},
	}
	for _, c := range cases {
		got, err := ParseToolCall(context.Background(), ParseToolCallOptions{ToolCall: c.call, Tools: c.tools})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Title != c.title || got.Invalid != c.invalid || len(got.ToolMetadata) != len(c.metadata) || (c.metadata != nil && got.ToolMetadata["m"] != 1) {
			t.Errorf("%s: title %q toolMetadata %v invalid %v; want %q %v %v", c.name, got.Title, got.ToolMetadata, got.Invalid, c.title, c.metadata, c.invalid)
		}
	}
}
