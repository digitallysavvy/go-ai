package ai

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r76genErrorValueTool returns a Go error as its result value (not as its
// error): TS execute returning an Error object.
func r76genErrorValueTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return errors.New("boom"), nil
		},
	}
}

// r76genCheckErrorValueStep checks a step whose tool returned a Go error as
// its result value: TS content is a tool-result whose output is the value,
// step.toolResults holds it, and the tool message carries
// createToolModelOutput's json toJSONValue(output) ({} for an Error).
func r76genCheckErrorValueStep(t *testing.T, step types.StepResult) {
	t.Helper()
	var result *types.ToolResultContent
	for _, part := range step.Content {
		switch p := part.(type) {
		case types.ToolErrorContent:
			t.Fatalf("content has a tool-error %#v, want a tool-result", p)
		case types.ToolResultContent:
			result = &p
		}
	}
	if result == nil {
		t.Fatalf("no tool-result in %#v", step.Content)
	}
	if err, ok := result.Result.(error); !ok || err.Error() != "boom" {
		t.Fatalf("Result = %#v, want the raw error value", result.Result)
	}
	want := &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{}}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("content Output = %#v, want %#v", result.Output, want)
	}
	if len(step.ToolResults) != 1 {
		t.Fatalf("ToolResults = %d, want 1", len(step.ToolResults))
	}
	msgs := step.Response.Messages
	if len(msgs) != 2 || msgs[1].Role != types.RoleTool {
		t.Fatalf("response messages %#v", msgs)
	}
	got, ok := msgs[1].Content[0].(types.ToolResultContent)
	if !ok || !reflect.DeepEqual(got.Output, want) {
		t.Fatalf("tool message part = %#v, want Output %#v", msgs[1].Content[0], want)
	}
}

// A tool that returns a Go error as its result value (not as its error) is a
// tool result, as TS treats an execute that returns an Error object. Go used
// to record a tool-error content part and an error-text model output while
// still counting the call in ToolResults and streaming a tool-result chunk.
func TestR76Gen_ToolReturningErrorValueIsToolResult(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}}
	t.Run("generate", func(t *testing.T) {
		var n atomic.Int32
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if n.Add(1) == 1 {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{r76genErrorValueTool()}, StopWhen: []StopCondition{StepCountIs(2)}})
		if err != nil {
			t.Fatal(err)
		}
		r76genCheckErrorValueStep(t, res.Steps[0])
	})
	t.Run("stream", func(t *testing.T) {
		step1 := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &call}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
		step2 := []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, step2), Prompt: "test-input", Tools: []types.Tool{r76genErrorValueTool()}, StopWhen: []StopCondition{StepCountIs(2)}})
		r76genCheckErrorValueStep(t, r.Steps()[0])
	})
	t.Run("builders", func(t *testing.T) {
		tr := types.ToolResult{ToolCallID: "call-1", ToolName: "tool1", Result: errors.New("boom")}
		for _, part := range []types.ContentPart{toolResultsToContentParts([]types.ToolResult{tr})[0], toolResultContentFromToolResult(tr)} {
			if _, ok := part.(types.ToolResultContent); !ok {
				t.Fatalf("part = %#v, want a tool-result", part)
			}
		}
	})
}

// R76 item 2 (carry CR cr-r76-carry-to-gen.md): with withoutCallMetadata
// (set from InternalOptions.ClientToolResultsWithoutCallMetadata for
// WorkflowAgent), executed and failed client tool results carry no provider
// metadata (TS WorkflowAgent executeTool);
// without it the call's metadata is copied (TS core execute-tool-call.ts).
func TestR76Gen_ClientToolResultsWithoutCallMetadata(t *testing.T) {
	md := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	exec := func(out interface{}, err error) types.ToolExecutor {
		return func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return out, err
		}
	}
	tools := []types.Tool{
		{Name: "ok", Parameters: map[string]interface{}{"type": "object"}, Execute: exec("ok", nil)},
		{Name: "fail", Parameters: map[string]interface{}{"type": "object"}, Execute: exec(nil, errors.New("boom"))},
	}
	calls := []types.ToolCall{
		{ID: "c1", ToolName: "ok", Arguments: map[string]interface{}{}, ProviderMetadata: md},
		{ID: "c2", ToolName: "fail", Arguments: map[string]interface{}{}, ProviderMetadata: md},
		{ID: "c3", ToolName: "ok", Invalid: true, Error: errors.New("bad input"), ProviderMetadata: md},
	}
	for _, without := range []bool{false, true} {
		results, err := executeTools(context.Background(), calls, tools, nil, nil, nil, &types.Usage{}, toolCallEventCallbacks{withoutCallMetadata: without})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Fatalf("%d results", len(results))
		}
		for _, r := range results {
			if without && r.ProviderMetadata != nil {
				t.Errorf("without: %s ProviderMetadata = %v, want nil", r.ToolCallID, r.ProviderMetadata)
			}
			if r.ToolCallID == "c3" {
				// Invalid calls never carry it (TestR76Gen_InvalidToolCallErrorWithoutCallMetadata).
				if r.ProviderMetadata != nil {
					t.Errorf("invalid call ProviderMetadata = %v, want nil", r.ProviderMetadata)
				}
				continue
			}
			if !without && !reflect.DeepEqual(r.ProviderMetadata, md) {
				t.Errorf("default: %s ProviderMetadata = %v, want the call's", r.ToolCallID, r.ProviderMetadata)
			}
		}
	}
}

// An invalid client tool call's tool error carries no provider metadata: TS
// generateText ("insert error tool outputs for invalid tool calls") and
// streamLanguageModelCall build the tool-error without the call's
// providerMetadata, so the tool message has no providerOptions.
func TestR76Gen_InvalidToolCallErrorWithoutCallMetadata(t *testing.T) {
	md := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	call := types.ToolCall{ID: "call-1", ToolName: "unknownTool", Arguments: map[string]interface{}{}, ProviderMetadata: md}
	check := func(t *testing.T, step types.StepResult) {
		t.Helper()
		var found bool
		for _, part := range step.Content {
			if p, ok := part.(types.ToolErrorContent); ok {
				found = true
				if len(p.ProviderMetadata) != 0 {
					t.Fatalf("tool-error ProviderMetadata = %s, want none", p.ProviderMetadata)
				}
			}
		}
		if !found {
			t.Fatalf("no tool-error in %#v", step.Content)
		}
		msgs := step.Response.Messages
		tr, ok := msgs[len(msgs)-1].Content[0].(types.ToolResultContent)
		if !ok || tr.ProviderOptions != nil {
			t.Fatalf("tool message part %#v, want no ProviderOptions", msgs[len(msgs)-1].Content[0])
		}
	}
	tools := []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}}
	t.Run("generate", func(t *testing.T) {
		var n atomic.Int32
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if n.Add(1) == 1 {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		check(t, res.Steps[0])
	})
	t.Run("stream", func(t *testing.T) {
		step1 := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &call}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1), Prompt: "test-input", Tools: tools})
		check(t, r.Steps()[0])
	})
}

// Owner ruling 2026-11-01 (decision 1, option c): a tool's ValidateInput
// replaces the Parameters check (TS jsonSchema(schema, {validate})). A failure
// is an InvalidToolInputError whose cause is the TypeValidationError TS
// safeValidateTypes builds, so the tool error text is TS getErrorMessage of
// it; a success supplies the input the tool runs with.
func r76genValidatingTool(executed *[]map[string]interface{}) types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
		ValidateInput: func(input interface{}) (interface{}, error) {
			m, _ := input.(map[string]interface{})
			if _, ok := m["value"].(string); !ok {
				return nil, errors.New("data must have required property 'value'")
			}
			return map[string]interface{}{"value": m["value"], "defaulted": true}, nil
		},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			*executed = append(*executed, in)
			return "ok", nil
		},
	}
}

const r76genValidateErrorText = "AI_InvalidToolInputError: Invalid input for tool tool1: AI_TypeValidationError: Type validation failed: Value: {\"b\":1,\"a\":2}.\nError message: Error: data must have required property 'value'"

func TestR76Gen_ToolValidateInput(t *testing.T) {
	model := func(raw string) *testutil.MockLanguageModel {
		var n atomic.Int32
		return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if n.Add(1) == 1 {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "tool1", RawArguments: raw}}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}}
	}
	t.Run("failure", func(t *testing.T) {
		var executed []map[string]interface{}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model(`{ "b": 1, "a": 2 }`), Prompt: "x", Tools: []types.Tool{r76genValidatingTool(&executed)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(executed) != 0 {
			t.Fatal("invalid call executed")
		}
		te, ok := res.Steps[0].Content[1].(types.ToolErrorContent)
		if !ok || te.ErrorText != r76genValidateErrorText {
			t.Fatalf("tool error = %#v\nwant ErrorText %q", res.Steps[0].Content[1], r76genValidateErrorText)
		}
		var ve *providererrors.ValidationError
		if !errors.As(te.Err, &ve) || ve.Error() != "Type validation failed: Value: {\"b\":1,\"a\":2}.\nError message: Error: data must have required property 'value'" {
			t.Fatalf("cause = %v", ve)
		}
	})
	t.Run("success", func(t *testing.T) {
		var executed []map[string]interface{}
		// "value" is valid for ValidateInput; the Parameters schema is not
		// consulted, so an extra key it would not mind is irrelevant.
		if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model(`{"value":"v"}`), Prompt: "x", Tools: []types.Tool{r76genValidatingTool(&executed)}}); err != nil {
			t.Fatal(err)
		}
		if len(executed) != 1 || !reflect.DeepEqual(executed[0], map[string]interface{}{"value": "v", "defaulted": true}) {
			t.Fatalf("executed with %#v", executed)
		}
	})
	t.Run("without ValidateInput the Parameters schema", func(t *testing.T) {
		var executed []map[string]interface{}
		tool := r76genValidatingTool(&executed)
		tool.ValidateInput = nil
		tool.Parameters = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model(`{"b":1}`), Prompt: "x", Tools: []types.Tool{tool}})
		if err != nil {
			t.Fatal(err)
		}
		te, ok := res.Steps[0].Content[1].(types.ToolErrorContent)
		// r79: the Parameters failure is a TypeValidationError too (TS
		// safeParseJSON with a schema); the message is Go's validator text.
		want := "AI_InvalidToolInputError: Invalid input for tool tool1: AI_TypeValidationError: Type validation failed: Value: {\"b\":1}.\nError message: Error: $.value: required: property is missing"
		if !ok || te.ErrorText != want {
			t.Fatalf("tool error = %#v", res.Steps[0].Content[1])
		}
	})
	t.Run("stream", func(t *testing.T) {
		var executed []map[string]interface{}
		step1 := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "b": 1, "a": 2 }`}}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1), Prompt: "x", Tools: []types.Tool{r76genValidatingTool(&executed)}})
		var te types.ToolErrorContent
		for _, p := range r.Steps()[0].Content {
			if v, ok := p.(types.ToolErrorContent); ok {
				te = v
			}
		}
		if te.ErrorText != r76genValidateErrorText || len(executed) != 0 {
			t.Fatalf("stream tool error = %#v", te)
		}
	})
}

// Stream CR item C (cr-r76-stream-to-gen.md): with executeMessages set
// (WorkflowAgent), Execute and the approval function get them while the
// start/end events keep messages (TS workflow-agent.ts: needsApproval and
// execute get iterMessages, the events getToolCallbackMessages(iterMessages)).
func TestR76Gen_ExecuteMessagesForToolAndApproval(t *testing.T) {
	cbMsgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prompt"}}}}
	execMsgs := append(append([]types.Message(nil), cbMsgs...), types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "assistant"}}})
	var execGot, approvalGot, startGot, endGot []types.Message
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			execGot = o.Messages
			return "ok", nil
		}}
	approval := types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
		approvalGot = o.Messages
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	})
	for _, withExec := range []bool{false, true} {
		cb := toolCallEventCallbacks{messages: cbMsgs,
			onStart:  func(_ context.Context, e OnToolCallStartEvent) { startGot = e.Messages },
			onFinish: func(_ context.Context, e OnToolCallFinishEvent) { endGot = e.Messages }}
		want := cbMsgs
		if withExec {
			cb.executeMessages = execMsgs
			want = execMsgs
		}
		if _, err := executeTools(context.Background(), []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}, []types.Tool{tool}, nil, nil, approval, &types.Usage{}, cb); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(execGot, want) || !reflect.DeepEqual(approvalGot, want) {
			t.Fatalf("withExec=%v: execute %d msgs, approval %d msgs, want %d", withExec, len(execGot), len(approvalGot), len(want))
		}
		if !reflect.DeepEqual(startGot, cbMsgs) || !reflect.DeepEqual(endGot, cbMsgs) {
			t.Fatalf("withExec=%v: events got %d/%d msgs, want %d", withExec, len(startGot), len(endGot), len(cbMsgs))
		}
	}
}
