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

// V061 item 23: an empty approval reason is kept end to end (TS `reason ??
// default` keeps ""). TS generateText at 30548ee7f9 under bun:
//
//	history denial, reason '': last prompt message
//	{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"execution-denied","reason":""}}]}
//	policy denial (toolApproval returns {type:'denied', reason:''}): next prompt
//	{"role":"tool","content":[{"type":"tool-result","toolCallId":"c2","toolName":"tool1","output":{"type":"execution-denied","reason":""}}]}
//	and the step's tool-approval-response part has "approved":false,"reason":"".
func TestR86bgenEmptyApprovalReasonKept(t *testing.T) {
	t.Run("history", func(t *testing.T) {
		var prompts [][]types.Message
		// The response part as decoded from TS JSON.
		var response types.ToolApprovalResponseContent
		if err := json.Unmarshal([]byte(`{"type":"tool-approval-response","approvalId":"id-1","approved":false,"reason":""}`), &response); err != nil {
			t.Fatal(err)
		}
		if !response.HasReason() {
			t.Fatal(`decoded "reason":"" is not kept`)
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r71genHelloModel(&prompts),
			Tools: r71genTool1(func() (interface{}, error) { return "x", nil }),
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}},
					types.ToolApprovalRequestContent{ApprovalID: "id-1", ToolCallID: "call-1"},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{response}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		last := prompts[0][len(prompts[0])-1]
		r71genCheckJSON(t, "last prompt message", r71genJSON(t, []types.Message{last}), []interface{}{
			r71genToolMessage(r71genObj{"type": "execution-denied", "reason": ""}),
		})
		if out := last.Content[0].(types.ToolResultContent).Output; out.ReasonOr("Tool call execution denied.") != "" {
			t.Fatalf("ReasonOr = %q, want the empty reason", out.ReasonOr("Tool call execution denied."))
		}
	})

	t.Run("policy", func(t *testing.T) {
		var prompts [][]types.Message
		calls := 0
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			prompts = append(prompts, o.Prompt.Messages)
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "c2", ToolName: "tool1", RawArguments: "{}"}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		}}
		empty := ""
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model,
			Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "x", nil
			}}},
			ToolApproval: types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &empty}
			}),
			Prompt:   "hi",
			StopWhen: []StopCondition{IsStepCount(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(prompts) != 2 {
			t.Fatalf("model calls = %d", len(prompts))
		}
		last := prompts[1][len(prompts[1])-1]
		r71genCheckJSON(t, "next prompt tool message", r71genJSON(t, []types.Message{last}), []interface{}{
			r71genObj{"role": "tool", "content": []interface{}{r71genObj{"type": "tool-result", "toolCallId": "c2", "toolName": "tool1", "output": r71genObj{"type": "execution-denied", "reason": ""}}}},
		})
		found := false
		for _, part := range res.Steps[0].Content {
			if resp, ok := part.(types.ToolApprovalResponseContent); ok {
				found = true
				data, _ := json.Marshal(resp)
				var obj map[string]interface{}
				_ = json.Unmarshal(data, &obj)
				if reason, ok := obj["reason"]; !ok || reason != "" || obj["approved"] != false {
					t.Fatalf("approval response = %s", data)
				}
			}
		}
		if !found {
			t.Fatalf("no tool-approval-response part in %#v", res.Steps[0].Content)
		}
	})
}
