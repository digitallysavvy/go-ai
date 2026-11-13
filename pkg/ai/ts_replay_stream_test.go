package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The tests in this file replay scenarios from the TS test suite
// (packages/ai/src/generate-text/stream-text.test.ts and generate-text.test.ts
// at b658e8503c) through StreamText/GenerateText with the same mock-model
// inputs, and compare against the expected parts, snapshots and callback order
// the TS tests assert.

// tsPartName renders a Go full-stream chunk as the TS part it corresponds to
// ("type" or "type:detail"). Go-only lifecycle markers (response-metadata,
// ai.stream.*) have no TS fullStream counterpart and render as "".
func tsPartName(c *provider.StreamChunk) string {
	switch c.Type {
	case provider.ChunkTypeResponseMetadata, provider.ChunkTypeFirstChunk, provider.ChunkTypeStreamFinish:
		return ""
	case provider.ChunkTypeText:
		return "text-delta:" + c.ID + ":" + c.Text
	case provider.ChunkTypeReasoning:
		return "reasoning-delta:" + c.ID + ":" + c.Text
	case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
		return string(c.Type) + ":" + c.ID
	case provider.ChunkTypeToolCall:
		return "tool-call:" + c.ToolCall.ID + ":" + c.ToolCall.ToolName
	case provider.ChunkTypeToolResult:
		return "tool-result:" + c.ToolResult.ToolCallID + ":" + fmt.Sprint(c.ToolResult.Result)
	case provider.ChunkTypeFinishStep:
		return "finish-step:" + string(c.FinishReason)
	case provider.ChunkTypeFinish:
		return "finish:" + string(c.FinishReason)
	default:
		return string(c.Type)
	}
}

func collectTSParts(t *testing.T, s provider.TextStream) []string {
	t.Helper()
	var parts []string
	for {
		c, err := s.Next()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if name := tsPartName(c); name != "" {
			parts = append(parts, name)
		}
	}
}

func usage(in, out int64) *types.Usage {
	total := in + out
	return &types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total}
}

func tool1() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}, "additionalProperties": false},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		},
	}
}

// stream-text.test.ts describe('options.stopWhen') > '2 steps: initial,
// tool-result'.
func TestTSReplay_StreamText_TwoStepsToolResult(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
					{Type: provider.ChunkTypeReasoningStart, ID: "0"},
					{Type: provider.ChunkTypeReasoning, ID: "0", Text: "thinking"},
					{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
	var order []string
	var finish OnFinishEvent
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{tool1()}, StopWhen: []StopCondition{StepCountIs(3)},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) {
			order = append(order, fmt.Sprintf("step-end:%d", e.StepNumber))
		},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			order = append(order, "end")
			finish = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectTSParts(t, result.FullStream())
	want := []string{
		"start", "start-step",
		"reasoning-start:0", "reasoning-delta:0:thinking", "reasoning-end:0",
		"tool-call:call-1:tool1", "tool-result:call-1:result1", "finish-step:tool-calls",
		"start-step",
		"text-start:1", "text-delta:1:Hello, ", "text-delta:1:world!", "text-end:1",
		"finish-step:stop", "finish:stop",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fullStream parts\n got %q\nwant %q", got, want)
	}

	// Step inputs (TS 'should contain correct step inputs'): the second call
	// replays the assistant reasoning + tool call and the tool result.
	if len(model.StreamCalls) != 2 {
		t.Fatalf("model calls = %d", len(model.StreamCalls))
	}
	msgs := model.StreamCalls[1].Prompt.Messages
	if len(msgs) != 3 || msgs[0].Role != types.RoleUser || msgs[1].Role != types.RoleAssistant || msgs[2].Role != types.RoleTool {
		t.Fatalf("step 2 prompt roles = %v", roles(msgs))
	}
	if k := contentKinds(msgs[1].Content); !reflect.DeepEqual(k, []string{"reasoning", "tool-call"}) {
		t.Fatalf("assistant content = %v", k)
	}
	tr, ok := msgs[2].Content[0].(types.ToolResultContent)
	if !ok || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "result1" {
		t.Fatalf("tool message = %#v", msgs[2].Content[0])
	}

	// Callbacks (TS 'onFinish should send correct information').
	if !reflect.DeepEqual(order, []string{"step-end:0", "step-end:1", "end"}) {
		t.Fatalf("callback order = %v", order)
	}
	if k := contentKinds(finish.Content); !reflect.DeepEqual(k, []string{"reasoning", "tool-call", "other", "text"}) {
		t.Fatalf("onEnd content = %v", k)
	}
	if len(finish.Reasoning) != 0 || finish.ReasoningText != "" {
		t.Fatalf("onEnd reasoning must be the final step's: %v %q", finish.Reasoning, finish.ReasoningText)
	}
	if len(finish.StaticToolCalls) != 1 || len(finish.StaticToolResults) != 1 || len(finish.Steps) != 2 {
		t.Fatalf("onEnd aggregates: calls=%d results=%d steps=%d", len(finish.StaticToolCalls), len(finish.StaticToolResults), len(finish.Steps))
	}
	if len(finish.ResponseMessages) != 3 || finish.FinalStep.StepNumber != 1 {
		t.Fatalf("onEnd responseMessages=%d finalStep=%d", len(finish.ResponseMessages), finish.FinalStep.StepNumber)
	}
	if *finish.Usage.TotalTokens != 26 || *finish.TotalUsage.TotalTokens != 26 {
		t.Fatalf("onEnd usage = %v / %v, want 26 (TS usage: totalUsage)", *finish.Usage.TotalTokens, *finish.TotalUsage.TotalTokens)
	}
	if result.Text() != "Hello, world!" {
		t.Fatalf("text = %q", result.Text())
	}
	steps := result.Steps()
	if steps[0].Response.ID != "id-0" || steps[1].Response.ID != "id-1" {
		t.Fatalf("step response ids = %q %q", steps[0].Response.ID, steps[1].Response.ID)
	}
	if steps[0].FinishReason != types.FinishReasonToolCalls || steps[1].FinishReason != types.FinishReasonStop {
		t.Fatalf("step finish reasons = %v %v", steps[0].FinishReason, steps[1].FinishReason)
	}
}

func roles(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, string(m.Role))
	}
	return out
}

var _ = strings.Join

func approvalTool(executed *int) types.Tool {
	t := tool1()
	t.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		*executed++
		return "result1", nil
	}
	return t
}

func helloModel(prompts *[][]types.Message) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			if prompts != nil {
				*prompts = append(*prompts, o.Prompt.Messages)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
		DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			if prompts != nil {
				*prompts = append(*prompts, o.Prompt.Messages)
			}
			return &types.GenerateResult{Text: "Hello, world!", FinishReason: types.FinishReasonStop, Usage: *usage(3, 10)}, nil
		},
	}
}

func tsApprovalHistory(approved bool) []types.Message {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			contentPartFromToolCall(call),
			types.ToolApprovalRequestContent{ApprovalID: "id-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "id-1", Approved: approved},
		}},
	}
}

// stream-text.test.ts 'when a single tool needs approval (user-defined)'.
func TestTSReplay_StreamText_ToolNeedsApproval(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
			}), nil
		},
	}
	executed := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectTSParts(t, result.FullStream())
	want := []string{"start", "start-step", "tool-call:call-1:tool1", "tool-approval-request", "finish-step:tool-calls", "finish:tool-calls"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fullStream\n got %q\nwant %q", got, want)
	}
	if executed != 0 {
		t.Fatalf("tool executed %d times before approval", executed)
	}
	if k := contentKinds(result.Content()); len(k) != 2 || k[0] != "tool-call" {
		t.Fatalf("content = %v, want [tool-call, tool-approval-request]", k)
	}
	if _, ok := result.Content()[1].(types.ToolApprovalRequestContent); !ok {
		t.Fatalf("content[1] = %T", result.Content()[1])
	}
	msgs := result.ResponseMessages()
	if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant || len(msgs[0].Content) != 2 {
		t.Fatalf("response messages = %#v", msgs)
	}
	if _, ok := msgs[0].Content[1].(types.ToolApprovalRequestContent); !ok {
		t.Fatalf("assistant content[1] = %T, want approval request", msgs[0].Content[1])
	}
}

// stream-text.test.ts 'when a call ... is approved': the tool runs before the
// first step, its result is the first part after `start`, and the model sees
// the tool result in the prompt.
func TestTSReplay_StreamText_ApprovedCallExecutesBeforeFirstStep(t *testing.T) {
	var prompts [][]types.Message
	executed := 0
	var prepareStepResponseMessages [][]types.Message
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: helloModel(&prompts), Messages: tsApprovalHistory(true), Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
		StopWhen:     []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			prepareStepResponseMessages = append(prepareStepResponseMessages, o.ResponseMessages)
			return o
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectTSParts(t, result.FullStream())
	want := []string{
		"start", "tool-result:call-1:result1", "start-step",
		"text-start:1", "text-delta:1:Hello, world!", "text-end:1", "finish-step:stop", "finish:stop",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fullStream\n got %q\nwant %q", got, want)
	}
	if executed != 1 {
		t.Fatalf("executed = %d", executed)
	}
	if len(prompts) != 1 || len(prompts[0]) != 3 || prompts[0][2].Role != types.RoleTool {
		t.Fatalf("prompts = %v", prompts)
	}
	if k := contentKinds(prompts[0][1].Content); !reflect.DeepEqual(k, []string{"tool-call"}) {
		t.Fatalf("assistant prompt content = %v (approval request must not be sent)", k)
	}
	tr := prompts[0][2].Content[0].(types.ToolResultContent)
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "result1" {
		t.Fatalf("tool result = %#v", tr)
	}
	rm := result.ResponseMessages()
	if len(rm) != 2 || rm[0].Role != types.RoleTool || rm[1].Role != types.RoleAssistant {
		t.Fatalf("response messages roles = %v, want [tool assistant]", roles(rm))
	}
	if len(prepareStepResponseMessages) != 1 || len(prepareStepResponseMessages[0]) != 1 || prepareStepResponseMessages[0][0].Role != types.RoleTool {
		t.Fatalf("prepareStep responseMessages = %v", prepareStepResponseMessages)
	}
}

// stream-text.test.ts 'when a call ... is denied': the tool is not executed and
// the prompt carries an execution-denied tool result; the full stream emits
// tool-output-denied.
func TestTSReplay_StreamText_DeniedCall(t *testing.T) {
	var prompts [][]types.Message
	executed := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: helloModel(&prompts), Messages: tsApprovalHistory(false), Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
		StopWhen:     []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectTSParts(t, result.FullStream())
	want := []string{
		"start", "tool-output-denied", "start-step",
		"text-start:1", "text-delta:1:Hello, world!", "text-end:1", "finish-step:stop", "finish:stop",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fullStream\n got %q\nwant %q", got, want)
	}
	if executed != 0 {
		t.Fatalf("denied tool executed")
	}
	tr := prompts[0][2].Content[0].(types.ToolResultContent)
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied {
		t.Fatalf("prompt tool result = %#v, want execution-denied", tr.Output)
	}
}

// generate-text.test.ts approval scenarios through GenerateText.
func TestTSReplay_GenerateText_Approvals(t *testing.T) {
	t.Run("needs approval", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
					FinishReason: types.FinishReasonToolCalls, Usage: *usage(3, 10),
				}, nil
			},
		}
		executed := 0
		r, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "test-input", Tools: []types.Tool{approvalTool(&executed)},
			ToolApproval: map[string]interface{}{"tool1": "user-approval"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if executed != 0 || len(r.Steps) != 1 {
			t.Fatalf("executed=%d steps=%d", executed, len(r.Steps))
		}
		if k := contentKinds(r.Content); len(k) != 2 || k[0] != "tool-call" {
			t.Fatalf("content = %v", k)
		}
		if _, ok := r.Content[1].(types.ToolApprovalRequestContent); !ok {
			t.Fatalf("content[1] = %T", r.Content[1])
		}
		if len(r.ResponseMessages) != 1 || r.ResponseMessages[0].Role != types.RoleAssistant {
			t.Fatalf("response messages = %v", roles(r.ResponseMessages))
		}
	})
	t.Run("approved", func(t *testing.T) {
		var prompts [][]types.Message
		executed := 0
		r, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: helloModel(&prompts), Messages: tsApprovalHistory(true), Tools: []types.Tool{approvalTool(&executed)},
			ToolApproval: map[string]interface{}{"tool1": "user-approval"}, StopWhen: []StopCondition{StepCountIs(3)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if executed != 1 || r.Text != "Hello, world!" {
			t.Fatalf("executed=%d text=%q", executed, r.Text)
		}
		if got := roles(r.ResponseMessages); !reflect.DeepEqual(got, []string{"tool", "assistant"}) {
			t.Fatalf("response messages = %v", got)
		}
		tr := prompts[0][2].Content[0].(types.ToolResultContent)
		if tr.Output == nil || tr.Output.Value != "result1" {
			t.Fatalf("prompt tool result = %#v", tr)
		}
	})
	t.Run("denied", func(t *testing.T) {
		var prompts [][]types.Message
		executed := 0
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: helloModel(&prompts), Messages: tsApprovalHistory(false), Tools: []types.Tool{approvalTool(&executed)},
			ToolApproval: map[string]interface{}{"tool1": "user-approval"}, StopWhen: []StopCondition{StepCountIs(3)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if executed != 0 {
			t.Fatal("denied tool executed")
		}
		tr := prompts[0][2].Content[0].(types.ToolResultContent)
		if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied {
			t.Fatalf("prompt tool result = %#v", tr.Output)
		}
	})
}

// abortingStream replays chunks, then cancels the caller's context and fails
// with the context error, like the TS mock that aborts and errors with an
// AbortError from pull().
type abortingStream struct {
	chunks []provider.StreamChunk
	cancel context.CancelFunc
	ctx    context.Context
}

func (s *abortingStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return &c, nil
	}
	s.cancel()
	return nil, s.ctx.Err()
}
func (s *abortingStream) Err() error   { return nil }
func (s *abortingStream) Close() error { return nil }

// stream-text.test.ts describe('abort signal') > 'abort in 2nd step': onAbort
// receives the completed first step, onError and onEnd stay silent and the
// full stream ends with an abort part.
func TestTSReplay_StreamText_AbortInSecondStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			call++
			if call == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			}
			return &abortingStream{ctx: ctx, cancel: cancel, chunks: []provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			}}, nil
		},
	}
	var aborts []GenerateTextAbortEvent
	var errs []error
	ended := 0
	result, err := StreamText(ctx, StreamTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{tool1()}, StopWhen: []StopCondition{StepCountIs(3)},
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) { aborts = append(aborts, e) },
		OnError:      func(_ context.Context, e error) { errs = append(errs, e) },
		OnEndEvent:   func(context.Context, OnFinishEvent) { ended++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	var last *provider.StreamChunk
	s := result.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if tsPartName(c) != "" {
			cc := *c
			last = &cc
		}
	}
	if last == nil || last.Type != provider.ChunkTypeAbort {
		t.Fatalf("last part = %v, want abort", last)
	}
	if len(aborts) != 1 {
		t.Fatalf("onAbort calls = %d", len(aborts))
	}
	if len(aborts[0].Steps) != 1 {
		t.Fatalf("onAbort steps = %d, want the completed first step", len(aborts[0].Steps))
	}
	if k := contentKinds(aborts[0].Steps[0].Content); !reflect.DeepEqual(k, []string{"tool-call", "other"}) {
		t.Fatalf("onAbort step content = %v", k)
	}
	if aborts[0].Steps[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("step finish reason = %v", aborts[0].Steps[0].FinishReason)
	}
	if len(errs) != 0 || ended != 0 {
		t.Fatalf("onError=%v onEnd=%d, want none", errs, ended)
	}
}

// stream-text.test.ts 'should fire the model-call callbacks before tool
// execution and step finish', plus the same order through GenerateText.
func TestTSReplay_CallbackOrderWithToolCall(t *testing.T) {
	wantOrder := []string{"onStepStart", "onLanguageModelCallStart", "onLanguageModelCallEnd", "onToolExecutionStart", "onToolExecutionEnd", "onStepFinish"}
	var order []string
	var mu sync.Mutex
	rec := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}
	var modelEnd LanguageModelCallEndEvent
	tool := tool1()
	tool.Execute = func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		return fmt.Sprint(args["value"]) + "-result", nil
	}
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test-arg"}}
	for _, api := range []string{"streamText", "generateText"} {
		order = nil
		model := &testutil.MockLanguageModel{
			ProviderName: "mock-provider", ModelName: "mock-model-id",
			DoStreamFunc: func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "response-1", ModelID: "response-model"}},
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "Before tool."},
					{Type: provider.ChunkTypeTextEnd, ID: "1"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			},
			DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "Before tool.", ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls, Usage: *usage(3, 10)}, nil
			},
		}
		onModelEnd := func(_ context.Context, e LanguageModelCallEndEvent) { rec("onLanguageModelCallEnd"); modelEnd = e }
		onStepStart := func(context.Context, OnStepStartEvent) { rec("onStepStart") }
		onModelStart := func(context.Context, LanguageModelCallStartEvent) { rec("onLanguageModelCallStart") }
		onToolStart := func(context.Context, OnToolCallStartEvent) { rec("onToolExecutionStart") }
		onToolEnd := func(context.Context, OnToolCallFinishEvent) { rec("onToolExecutionEnd") }
		onStepEnd := func(context.Context, OnStepFinishEvent) { rec("onStepFinish") }
		if api == "streamText" {
			r, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "test-input", Tools: []types.Tool{tool},
				OnStepStart: onStepStart, OnLanguageModelCallStart: onModelStart, OnLanguageModelCallEnd: onModelEnd,
				OnToolExecutionStart: onToolStart, OnToolExecutionEnd: onToolEnd, OnStepEndEvent: onStepEnd,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
		} else {
			_, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: model, Prompt: "test-input", Tools: []types.Tool{tool},
				OnStepStart: onStepStart, OnLanguageModelCallStart: onModelStart, OnLanguageModelCallEnd: onModelEnd,
				OnToolExecutionStart: onToolStart, OnToolExecutionEnd: onToolEnd, OnStepEndEvent: onStepEnd,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(order, wantOrder) {
			t.Fatalf("%s callback order\n got %v\nwant %v", api, order, wantOrder)
		}
		if k := contentKinds(modelEnd.Content); !reflect.DeepEqual(k, []string{"text", "tool-call"}) {
			t.Fatalf("%s model-call end content = %v, want [text tool-call]", api, k)
		}
		if modelEnd.ResponseID != "response-1" && api == "streamText" {
			t.Fatalf("model-call end response id = %q", modelEnd.ResponseID)
		}
		if modelEnd.FinishReason != types.FinishReasonToolCalls {
			t.Fatalf("finish reason = %v", modelEnd.FinishReason)
		}
	}
}

// generate-text.test.ts describe('result.content') > 'should generate content':
// a provider that returns every part in Content, text included, keeps the
// model's order (text, source, file, reasoning, tool-call, text) and the tool
// result follows. result.text joins the text parts of the final step.
func TestTSReplay_GenerateText_ContentOrder(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}
	n := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			n++
			if n > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{
				Text:         "Hello, world!More text",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{call},
				Content: []types.ContentPart{
					types.TextContent{Text: "Hello, world!"},
					types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example"},
					types.GeneratedFileContent{MediaType: "image/png", Data: []byte{1, 2, 3}},
					types.ReasoningContent{Text: "I will open the conversation with witty banter."},
					contentPartFromToolCall(call),
					types.TextContent{Text: "More text"},
				},
			}, nil
		},
	}
	r, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "prompt", Tools: []types.Tool{tool1()}, StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"text", "source", "other", "reasoning", "tool-call", "text", "other"}
	if got := contentKinds(r.Content); !reflect.DeepEqual(got, want) {
		t.Fatalf("content kinds = %v, want %v", got, want)
	}
	if r.Steps[0].Text != "Hello, world!More text" {
		t.Fatalf("step text = %q, want the joined text parts", r.Steps[0].Text)
	}
	if len(r.Files) != 1 || len(r.Sources) != 1 {
		t.Fatalf("files=%d sources=%d", len(r.Files), len(r.Sources))
	}
}

// A provider that fills Content with text parts but leaves Text empty still
// yields the step text TS derives from the text parts.
func TestGenerateText_TextDerivedFromContentTextParts(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonStop,
				Content:      []types.ContentPart{types.TextContent{Text: "Hello, "}, types.ReasoningContent{Text: "hm"}, types.TextContent{Text: "world!"}},
			}, nil
		},
	}
	r, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "Hello, world!" || r.Steps[0].Text != "Hello, world!" {
		t.Fatalf("text = %q / step %q", r.Text, r.Steps[0].Text)
	}
}

func toolResultTexts(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok && tr.Output != nil {
				out = append(out, string(tr.Output.Type)+":"+fmt.Sprint(tr.Output.Value))
			}
		}
	}
	return out
}

// generate-text.test.ts describe('tool execution errors') and
// describe('invalid tool calls') through GenerateText and StreamText.
func TestTSReplay_ToolErrors(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}
	failing := tool1()
	failing.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return nil, errors.New("test error")
	}
	gen := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls, Usage: *usage(10, 20)}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &call},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(10, 20)},
			}), nil
		},
	}
	check := func(t *testing.T, content []types.ContentPart, msgs []types.Message) {
		t.Helper()
		if len(content) != 2 {
			t.Fatalf("content = %v", contentKinds(content))
		}
		if _, ok := content[0].(types.ToolCallContent); !ok {
			t.Fatalf("content[0] = %T", content[0])
		}
		te, ok := content[1].(types.ToolErrorContent)
		if !ok || te.ToolCallID != "call-1" {
			t.Fatalf("content[1] = %#v, want tool-error", content[1])
		}
		if got := toolResultTexts(msgs); !reflect.DeepEqual(got, []string{"error-text:Error: test error"}) {
			t.Fatalf("response tool outputs = %v, want error-text 'Error: test error'", got)
		}
		if len(msgs) != 2 || msgs[0].Role != types.RoleAssistant || msgs[1].Role != types.RoleTool {
			t.Fatalf("roles = %v", roles(msgs))
		}
	}
	t.Run("generateText", func(t *testing.T) {
		r, err := GenerateText(context.Background(), GenerateTextOptions{Model: gen, Prompt: "test-input", Tools: []types.Tool{failing}})
		if err != nil {
			t.Fatal(err)
		}
		check(t, r.Content, r.ResponseMessages)
	})
	t.Run("streamText", func(t *testing.T) {
		r, err := StreamText(context.Background(), StreamTextOptions{Model: gen, Prompt: "test-input", Tools: []types.Tool{failing}})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		check(t, r.Content(), r.ResponseMessages())
	})
}

func TestTSReplay_InvalidToolCallSkipsApprovalAndExecution(t *testing.T) {
	bad := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"cities": "San Francisco"}}
	executed := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{bad}, FinishReason: types.FinishReasonToolCalls, Usage: *usage(10, 20)}, nil
		},
	}
	r, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "prompt", Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 {
		t.Fatal("invalid call executed")
	}
	for _, p := range r.Content {
		if _, ok := p.(types.ToolApprovalRequestContent); ok {
			t.Fatal("invalid call requested approval")
		}
	}
	tc, ok := r.Content[0].(types.ToolCallContent)
	if !ok || !tc.Invalid || !tc.Dynamic {
		t.Fatalf("content[0] = %#v, want invalid dynamic tool-call", r.Content[0])
	}
	if _, ok := r.Content[1].(types.ToolErrorContent); !ok {
		t.Fatalf("content[1] = %T, want tool-error", r.Content[1])
	}
	outs := toolResultTexts(r.ResponseMessages)
	if len(outs) != 1 || !strings.HasPrefix(outs[0], "error-text:AI_InvalidToolInputError: Invalid input for tool tool1: ") {
		t.Fatalf("response tool outputs = %v", outs)
	}
}

// stream-text.test.ts describe('options.onChunk') > 'should return events in
// order'.
func TestTSReplay_StreamText_OnChunkOrder(t *testing.T) {
	meta := json.RawMessage(`{"provider":{"custom":"value"}}`)
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeToolInputStart, ID: "2", ToolCall: &types.ToolCall{ID: "2", ToolName: "tool1"}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "2", Text: `{"value": "`},
				{Type: provider.ChunkTypeReasoningStart, ID: "3"},
				{Type: provider.ChunkTypeReasoning, ID: "3", Text: "Feeling clever"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "3"},
				{Type: provider.ChunkTypeToolInputDelta, ID: "2", Text: "test"},
				{Type: provider.ChunkTypeToolInputDelta, ID: "2", Text: `"}`},
				{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example"}},
				{Type: provider.ChunkTypeToolInputEnd, ID: "2", ToolCall: &types.ToolCall{ID: "2"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}, ProviderMetadata: map[string]interface{}{"provider": map[string]interface{}{"custom": "value"}}}, ProviderMetadata: meta},
				{Type: provider.ChunkTypeTextStart, ID: "4"},
				{Type: provider.ChunkTypeText, ID: "4", Text: " World"},
				{Type: provider.ChunkTypeTextEnd, ID: "4"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
	var got []string
	echo := tool1()
	echo.Execute = func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		return fmt.Sprint(args["value"]) + "-result", nil
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{echo},
		OnChunk: func(c provider.StreamChunk) {
			switch c.Type {
			case provider.ChunkTypeToolInputStart:
				got = append(got, "tool-input-start:"+c.ID+":"+c.ToolCall.ToolName)
			case provider.ChunkTypeToolInputDelta:
				got = append(got, "tool-input-delta:"+c.ID+":"+c.Text)
			case provider.ChunkTypeToolInputEnd:
				got = append(got, "tool-input-end:"+c.ID)
			case provider.ChunkTypeSource:
				got = append(got, "source:"+c.SourceContent.ID)
			default:
				if name := tsPartName(&c); name != "" {
					got = append(got, name)
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"start", "start-step",
		"text-start:1", "text-delta:1:Hello", "text-end:1",
		"tool-input-start:2:tool1", `tool-input-delta:2:{"value": "`,
		"reasoning-start:3", "reasoning-delta:3:Feeling clever", "reasoning-end:3",
		"tool-input-delta:2:test", `tool-input-delta:2:"}`,
		"source:123", "tool-input-end:2", "tool-call:2:tool1",
		"text-start:4", "text-delta:4: World", "text-end:4",
		"tool-result:2:test-result", "finish-step:stop", "finish:stop",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("onChunk parts\n got %q\nwant %q", got, want)
	}
}

// stream-text.test.ts describe('errors').
func TestTSReplay_StreamText_Errors(t *testing.T) {
	t.Run("doStream failure is an error part after start and reaches onError", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, errors.New("test error")
			},
		}
		var reported []error
		result, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "test-input",
			OnError: func(_ context.Context, e error) { reported = append(reported, e) },
		})
		if err != nil {
			t.Fatalf("StreamText returned %v; TS swallows the failure into the stream", err)
		}
		var parts []string
		s := result.FullStream()
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				parts = append(parts, "error")
			} else if name := tsPartName(c); name != "" {
				parts = append(parts, name)
			}
		}
		if !reflect.DeepEqual(parts, []string{"start", "error"}) {
			t.Fatalf("parts = %v, want [start error]", parts)
		}
		if len(reported) != 1 || !strings.Contains(reported[0].Error(), "test error") {
			t.Fatalf("onError = %v", reported)
		}
	})
	// TS reports `text part <id> not found` for a delta without a start part.
	// The Go provider stream protocol lets providers send deltas without
	// start/end boundaries (core opens an implicit block), so that error does
	// not exist here.
	t.Run("onEnd still runs after a mid-stream error chunk", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
					{Type: provider.ChunkTypeError, Err: errors.New("chunk error")},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError, Usage: usage(3, 10)},
				}), nil
			},
		}
		var reported []error
		var finish *OnFinishEvent
		result, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "test-input",
			OnError:    func(_ context.Context, e error) { reported = append(reported, e) },
			OnEndEvent: func(_ context.Context, e OnFinishEvent) { finish = &e },
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = result.ConsumeStream()
		if len(reported) != 1 || !strings.Contains(reported[0].Error(), "chunk error") {
			t.Fatalf("onError = %v", reported)
		}
		if finish == nil || finish.FinishReason != types.FinishReasonError || finish.Text != "Hello" || *finish.TotalUsage.TotalTokens != 13 {
			t.Fatalf("onEnd event = %#v", finish)
		}
	})
}

// generate-text.test.ts 'tool choice enforcement' > 'should reject when a
// required tool choice produces no tool call': the error carries the whole
// response content, reasoning and text, whichever shape the provider used.
func TestTSReplay_GenerateText_ToolChoiceViolationContent(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "No tool call.",
				Content:      []types.ContentPart{types.ReasoningContent{Text: "I will not call the tool."}},
				FinishReason: types.FinishReasonStop, Usage: *usage(3, 10),
			}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{tool1()}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	})
	var v *ToolChoiceViolationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v", err)
	}
	if v.Error() != "Model response did not contain a tool call even though tool choice was required." || v.FinishReason != types.FinishReasonStop || v.Provider != "mock-provider" || v.ModelID != "mock-model-id" {
		t.Fatalf("violation = %#v", v)
	}
	if k := contentKinds(v.Content); !reflect.DeepEqual(k, []string{"reasoning", "text"}) {
		t.Fatalf("violation content = %v, want [reasoning text]", k)
	}
}
