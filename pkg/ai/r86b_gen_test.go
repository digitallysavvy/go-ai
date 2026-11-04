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

// r86bgenDecide runs ExperimentalDecide with a model returning answers.
func r86bgenDecide(questions map[string]provider.DecisionQuestion, answers map[string]provider.DecisionAnswer) (*DecideResult, *mockDecisionModel, error) {
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: answers}, nil
		},
	}
	result, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "s", Questions: questions})
	return result, mock, err
}

// V061 items 30 and 32, against TS decide at 30548ee7f9 under bun
// (DecisionMockModelV4):
//
//	emptyChoice ok {"q":{"type":"choice","choice":""}}
//	emptyChoiceNotOption error AI_InvalidResponseDataError Question "q" selected an unknown option.
//	scoreStrings ok {"q":{"type":"score","score":1.5}}
func TestR86bgenDecideEmptyChoiceAndTypedCriteria(t *testing.T) {
	result, _, err := r86bgenDecide(
		map[string]provider.DecisionQuestion{"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"": "empty", "a": "A"}}},
		map[string]provider.DecisionAnswer{"q": {Type: "choice", Choice: ""}},
	)
	if err != nil || result.Answers["q"].Choice != "" {
		t.Fatalf("emptyChoice: %v, %v", result, err)
	}
	_, _, err = r86bgenDecide(
		map[string]provider.DecisionQuestion{"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": "A"}}},
		map[string]provider.DecisionAnswer{"q": {Type: "choice", Choice: ""}},
	)
	if err == nil || err.Error() != `Question "q" selected an unknown option.` {
		t.Fatalf("emptyChoiceNotOption: %v", err)
	}

	score := 1.5
	type r86bgenLevel string
	for name, criteria := range map[string]interface{}{
		"[]string":       []string{"low", "mid", "high"},
		"named elements": []r86bgenLevel{"low", "mid", "high"},
		"array":          [3]string{"low", "mid", "high"},
	} {
		_, mock, err := r86bgenDecide(
			map[string]provider.DecisionQuestion{"q": {Type: "score", Instructions: "i", Criteria: criteria}},
			map[string]provider.DecisionAnswer{"q": {Type: "score", Score: &score}},
		)
		if err != nil {
			t.Fatalf("scoreStrings %s: %v", name, err)
		}
		// The model gets the JSON form the providers read.
		if got, ok := mock.calls[0].Questions["q"].Criteria.([]interface{}); !ok || len(got) != 3 || got[2] != "high" {
			t.Fatalf("scoreStrings %s: model criteria = %#v", name, mock.calls[0].Questions["q"].Criteria)
		}
	}

	// Same class: typed choice options and instructions.
	_, mock, err := r86bgenDecide(
		map[string]provider.DecisionQuestion{"q": {Type: "choice", Instructions: []string{"pick", "one"}, Criteria: map[string]string{"a": "A", "b": "B"}}},
		map[string]provider.DecisionAnswer{"q": {Type: "choice", Choice: "b"}},
	)
	if err != nil {
		t.Fatalf("typed choice: %v", err)
	}
	if got, ok := mock.calls[0].Questions["q"].Criteria.(map[string]interface{}); !ok || got["b"] != "B" {
		t.Fatalf("typed choice criteria = %#v", mock.calls[0].Questions["q"].Criteria)
	}
}

// V061 item 33: one completion-order helper. Results listed in the
// completion order move into that order among their own positions; the
// others (results not streamed as they settled) keep theirs.
func TestR86bgenToolResultsInCompletionOrder(t *testing.T) {
	ids := func(results []types.ToolResult) string {
		out := ""
		for _, r := range results {
			out += r.ToolCallID
		}
		return out
	}
	mk := func(s string) []types.ToolResult {
		var out []types.ToolResult
		for _, c := range s {
			out = append(out, types.ToolResult{ToolCallID: string(c)})
		}
		return out
	}
	for _, c := range []struct {
		results, completed, want string
	}{
		{"ab", "ba", "ba"},
		{"abc", "cab", "cab"},
		{"axb", "ba", "bxa"},   // x was not streamed live: keeps its slot
		{"abc", "c", "abc"},    // one listed result: nothing to reorder
		{"abc", "cbac", "cba"}, // first occurrence counts
		{"a", "a", "a"},
	} {
		in := mk(c.results)
		got := resultsInCompletionOrder(in, r86bgenSplitChars(c.completed))
		if ids(got) != c.want {
			t.Errorf("%s by %s = %s, want %s", c.results, c.completed, ids(got), c.want)
		}
		if ids(in) != c.results {
			t.Errorf("input changed to %s", ids(in))
		}
	}
}

func r86bgenSplitChars(s string) []string {
	var out []string
	for _, c := range s {
		out = append(out, string(c))
	}
	return out
}
