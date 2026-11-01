package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// R75 item 2: with a ModelOutput set, the raw tool result stays in
// ToolResultContent.Result even when it is itself a ToolResultOutput (TS step
// content tool-result `output` is the raw value), in both pkg/ai builders.
func TestR75Gen_ContentKeepsRawToolResultOutputWithModelOutput(t *testing.T) {
	raw := types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "raw"}
	model := &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model"}
	for _, rawValue := range []interface{}{raw, &raw} {
		tr := types.ToolResult{ToolCallID: "c1", ToolName: "t", Result: rawValue, ModelOutput: model}
		parts := toolResultsToContentParts([]types.ToolResult{tr})
		if len(parts) != 1 {
			t.Fatalf("toolResultsToContentParts: %d parts", len(parts))
		}
		for name, p := range map[string]types.ContentPart{"toolResultsToContentParts": parts[0], "toolResultContentFromToolResult": toolResultContentFromToolResult(tr)} {
			got, ok := p.(types.ToolResultContent)
			if !ok {
				t.Fatalf("%s: got %T", name, p)
			}
			if !reflect.DeepEqual(got.Result, rawValue) || got.Output != model {
				t.Fatalf("%s: Result=%#v Output=%#v", name, got.Result, got.Output)
			}
		}
		// Without a model output the ToolResultOutput is the output itself.
		tr.ModelOutput = nil
		for name, p := range map[string]types.ContentPart{"toolResultsToContentParts": toolResultsToContentParts([]types.ToolResult{tr})[0], "toolResultContentFromToolResult": toolResultContentFromToolResult(tr)} {
			got := p.(types.ToolResultContent)
			if got.Result != nil || got.Output == nil || got.Output.Value != "raw" {
				t.Fatalf("%s without model output: Result=%#v Output=%#v", name, got.Result, got.Output)
			}
		}
	}
}

// generate-text.test.ts 8739, 8800, 8829, 8852 (provider-executed tools > two
// provider-executed tool calls and results): content, staticToolCalls,
// staticToolResults (errors excluded) and a single step.
func TestR75Gen_TwoProviderExecutedCallsAndResults(t *testing.T) {
	isErr := true
	meta := func(id string) map[string]interface{} {
		return map[string]interface{}{"openai": map[string]interface{}{"itemId": id}}
	}
	metaJSON := func(id string) json.RawMessage {
		b, _ := json.Marshal(meta(id))
		return b
	}
	call := func(id string) types.ToolCall {
		return types.ToolCall{ID: id, ToolName: "web_search", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`, ProviderExecuted: true}
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, &types.GenerateResult{
			FinishReason: types.FinishReasonStop,
			Usage:        *usage(3, 10),
			ToolCalls:    []types.ToolCall{call("call-1"), call("call-2")},
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "web_search", Input: `{ "value": "value" }`, ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "call-1", ToolName: "web_search", Result: `{ "value": "result1" }`, ProviderMetadata: metaJSON("tool-result-1")},
				types.ToolCallContent{ToolCallID: "call-2", ToolName: "web_search", Input: `{ "value": "value" }`, ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "call-2", ToolName: "web_search", Result: "ERROR", IsError: &isErr, ProviderExecuted: true, ProviderMetadata: metaJSON("tool-result-2")},
			},
		}),
		Tools: []types.Tool{{
			Name: "web_search", Type: types.ToolTypeProviderDefined, ProviderID: "test.web_search", ProviderExecuted: true,
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		}},
		Prompt:   "test-input",
		StopWhen: []StopCondition{StepCountIs(4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(res.Steps))
	}
	if len(res.Content) != 4 {
		t.Fatalf("content = %#v", res.Content)
	}
	wantInput := map[string]interface{}{"value": "value"}
	for i, id := range []string{"call-1", "call-2"} {
		c, ok := res.Content[2*i].(types.ToolCallContent)
		if !ok || c.ToolCallID != id || c.ToolName != "web_search" || !c.ProviderExecuted || c.Dynamic || c.Invalid {
			t.Fatalf("content[%d] = %#v", 2*i, res.Content[2*i])
		}
	}
	r1, ok := res.Content[1].(types.ToolResultContent)
	if !ok || r1.ToolCallID != "call-1" || !r1.ProviderExecuted || r1.Dynamic || r1.Result != `{ "value": "result1" }` || !reflect.DeepEqual(r1.Input, wantInput) {
		t.Fatalf("content[1] = %#v", res.Content[1])
	}
	var gotMeta interface{}
	if err := json.Unmarshal(r1.ProviderMetadata, &gotMeta); err != nil || !reflect.DeepEqual(gotMeta, meta("tool-result-1")) {
		t.Fatalf("content[1] metadata = %s", r1.ProviderMetadata)
	}
	e2, ok := res.Content[3].(types.ToolErrorContent)
	if !ok || e2.ToolCallID != "call-2" || !e2.ProviderExecuted || e2.Dynamic || e2.Error != "ERROR" || !reflect.DeepEqual(e2.Input, wantInput) {
		t.Fatalf("content[3] = %#v", res.Content[3])
	}
	if err := json.Unmarshal(e2.ProviderMetadata, &gotMeta); err != nil || !reflect.DeepEqual(gotMeta, meta("tool-result-2")) {
		t.Fatalf("content[3] metadata = %s", e2.ProviderMetadata)
	}
	if len(res.StaticToolCalls) != 2 || res.StaticToolCalls[0].ID != "call-1" || res.StaticToolCalls[1].ID != "call-2" || !res.StaticToolCalls[0].ProviderExecuted || !res.StaticToolCalls[1].ProviderExecuted {
		t.Fatalf("static tool calls = %#v", res.StaticToolCalls)
	}
	if len(res.StaticToolResults) != 1 {
		t.Fatalf("static tool results = %#v, want call-1 only", res.StaticToolResults)
	}
	sr := res.StaticToolResults[0]
	if sr.ToolCallID != "call-1" || sr.ToolName != "web_search" || !sr.ProviderExecuted || sr.Dynamic || sr.Result != `{ "value": "result1" }` || !reflect.DeepEqual(sr.Input, wantInput) || !reflect.DeepEqual(sr.ProviderMetadata, meta("tool-result-1")) {
		t.Fatalf("static tool result = %#v", sr)
	}
}

// generate-text.test.ts 12429-12543 (when a single tool has a needsApproval
// function): one step, tool-calls finish, the approval request for call-1
// only, and the per-tool approval function sees each call's input with
// toolCallId, the step messages, the runtime context and no tool context.
func TestR75Gen_PerToolApprovalFunctionInputAndOptions(t *testing.T) {
	type approvalCall struct {
		args map[string]interface{}
		opts types.SingleToolApprovalOptions
	}
	var mu sync.Mutex
	var calls []approvalCall
	runtime := map[string]interface{}{}
	call := func(id, value string) types.ToolCall {
		return types.ToolCall{ID: id, ToolName: "tool1", Arguments: map[string]interface{}{"value": value}, RawArguments: `{ "value": "` + value + `" }`}
	}
	tool := r72genValueTool("tool1")
	tool.Execute = func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		return "result for " + args["value"].(string), nil
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:          r72genSeqModel(t, r72genCalls(call("call-1", "value-needs-approval"), call("call-2", "value-no-approval"))),
		Tools:          []types.Tool{tool},
		RuntimeContext: runtime,
		ToolApproval: map[string]types.ToolApprovalValue{
			"tool1": types.SingleToolApprovalFunc(func(args map[string]interface{}, opts types.SingleToolApprovalOptions) types.ToolApprovalResult {
				mu.Lock()
				calls = append(calls, approvalCall{args, opts})
				mu.Unlock()
				if args["value"] == "value-needs-approval" {
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
				}
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
			}),
		},
		StopWhen: []StopCondition{StepCountIs(3)},
		Prompt:   "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("steps = %d, finish = %q", len(res.Steps), res.FinishReason)
	}
	var requests []types.ToolApprovalRequestContent
	var results []types.ToolResultContent
	for _, p := range res.Content {
		switch v := p.(type) {
		case types.ToolApprovalRequestContent:
			requests = append(requests, v)
		case types.ToolResultContent:
			results = append(results, v)
		}
	}
	if len(requests) != 1 || requests[0].ToolCallID != "call-1" {
		t.Fatalf("approval requests = %#v", requests)
	}
	if len(results) != 1 || results[0].ToolCallID != "call-2" || results[0].Result != "result for value-no-approval" {
		t.Fatalf("tool results = %#v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("approval calls = %d, want 2", len(calls))
	}
	byID := map[string]approvalCall{}
	for _, c := range calls {
		byID[c.opts.ToolCallID] = c
	}
	for id, value := range map[string]string{"call-1": "value-needs-approval", "call-2": "value-no-approval"} {
		c, ok := byID[id]
		if !ok {
			t.Fatalf("no approval call for %s", id)
		}
		if !reflect.DeepEqual(c.args, map[string]interface{}{"value": value}) {
			t.Fatalf("%s input = %#v", id, c.args)
		}
		if c.opts.ToolContext != nil || !reflect.DeepEqual(c.opts.RuntimeContext, runtime) {
			t.Fatalf("%s toolContext = %#v, runtimeContext = %#v", id, c.opts.ToolContext, c.opts.RuntimeContext)
		}
		if g := r72genTSMessages(t, c.opts.Messages); !reflect.DeepEqual(g, r72genJSON(t, []interface{}{map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "test-input"}}}})) {
			gj, _ := json.Marshal(g)
			t.Fatalf("%s messages = %s", id, gj)
		}
	}
}
