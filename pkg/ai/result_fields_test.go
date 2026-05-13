package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestBuildReasoningTextAndToolFilters(t *testing.T) {
	reasoning := []types.ReasoningContent{
		{Text: "step 1"},
		{Text: ""},
		{Text: "step 2"},
	}
	if got := buildReasoningText(reasoning); got != "step 1\nstep 2" {
		t.Fatalf("buildReasoningText() = %q", got)
	}

	toolCalls := []types.ToolCall{
		{ID: "a", ToolName: "static", Dynamic: false},
		{ID: "b", ToolName: "dynamic", Dynamic: true},
	}
	if got := filterStaticToolCalls(toolCalls); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("filterStaticToolCalls() = %+v", got)
	}
	if got := filterDynamicToolCalls(toolCalls); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("filterDynamicToolCalls() = %+v", got)
	}

	toolResults := []types.ToolResult{
		{ToolCallID: "a", ToolName: "static", Dynamic: false},
		{ToolCallID: "b", ToolName: "dynamic", Dynamic: true},
	}
	if got := filterStaticToolResults(toolResults); len(got) != 1 || got[0].ToolCallID != "a" {
		t.Fatalf("filterStaticToolResults() = %+v", got)
	}
	if got := filterDynamicToolResults(toolResults); len(got) != 1 || got[0].ToolCallID != "b" {
		t.Fatalf("filterDynamicToolResults() = %+v", got)
	}
}

func TestGenerateTextPopulatesStepAndFinalFields(t *testing.T) {
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	rawReq := map[string]interface{}{"input": "hello"}
	rawResp := map[string]interface{}{"finish_reason": "stop", "id": "raw-1"}
	responseHeaders := map[string]string{"x-request-id": "req-1"}
	metadata := map[string]interface{}{"provider": map[string]interface{}{"flag": true}}

	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider",
		ModelName:    "mock-model",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			in := int64(10)
			out := int64(5)
			total := int64(15)
			return &types.GenerateResult{
				Text: "final answer",
				Content: []types.ContentPart{
					types.ReasoningContent{Text: "reasoning-a"},
					types.ReasoningContent{Text: "reasoning-b"},
				},
				ToolCalls: []types.ToolCall{
					{ID: "tc-static", ToolName: "lookup", Dynamic: false},
					{ID: "tc-dyn", ToolName: "search", Dynamic: true},
				},
				FinishReason:     types.FinishReasonStop,
				Usage:            types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total},
				RawRequest:       rawReq,
				RawResponse:      rawResp,
				ResponseHeaders:  responseHeaders,
				ProviderMetadata: metadata,
				ResponseMetadata: &types.ResponseMetadata{
					ID:        "resp-123",
					Timestamp: ts,
					ModelID:   "mock-model",
					Headers:   responseHeaders,
				},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}

	if result.ReasoningText != "reasoning-a\nreasoning-b" || len(result.Reasoning) != 2 {
		t.Fatalf("unexpected final reasoning fields: %+v", result)
	}
	if len(result.StaticToolCalls) != 1 || result.StaticToolCalls[0].ID != "tc-static" {
		t.Fatalf("unexpected static tool calls: %+v", result.StaticToolCalls)
	}
	if len(result.DynamicToolCalls) != 1 || result.DynamicToolCalls[0].ID != "tc-dyn" {
		t.Fatalf("unexpected dynamic tool calls: %+v", result.DynamicToolCalls)
	}
	if result.Request.Body == nil || result.Response.ID != "resp-123" || result.Response.ModelID != "mock-model" {
		t.Fatalf("unexpected final request/response fields: request=%+v response=%+v", result.Request, result.Response)
	}
	if result.RawFinishReason != "stop" {
		t.Fatalf("RawFinishReason = %q, want stop", result.RawFinishReason)
	}

	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(result.Steps))
	}
	step := result.Steps[0]
	if step.ReasoningText != "reasoning-a\nreasoning-b" {
		t.Fatalf("step reasoning text = %q", step.ReasoningText)
	}
	if len(step.StaticToolCalls) != 1 || len(step.DynamicToolCalls) != 1 {
		t.Fatalf("unexpected step tool call split: static=%+v dynamic=%+v", step.StaticToolCalls, step.DynamicToolCalls)
	}
	if step.Request.Body == nil || step.Response.ID != "resp-123" {
		t.Fatalf("unexpected step request/response: request=%+v response=%+v", step.Request, step.Response)
	}
}

func TestStreamTextResultAccessorFields(t *testing.T) {
	req := types.StepRequest{Body: map[string]interface{}{"input": "hi"}}
	resp := types.StepResponse{
		ID:        "resp-stream-1",
		Timestamp: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
		ModelID:   "mock-stream-model",
		Headers:   map[string]string{"x-request-id": "stream-1"},
	}
	r := &StreamTextResult{
		stream: testutil.NewMockTextStream(nil),
		toolCalls: []types.ToolCall{
			{ID: "a", Dynamic: false},
			{ID: "b", Dynamic: true},
		},
		toolResults: []types.ToolResult{
			{ToolCallID: "a", Dynamic: false},
			{ToolCallID: "b", Dynamic: true},
		},
		rawFinishReason: "length",
		responseHeaders: map[string]string{"x-request-id": "stream-1"},
		stepRequest:     req,
		stepResponse:    resp,
	}

	if len(r.StaticToolCalls()) != 1 || len(r.DynamicToolCalls()) != 1 {
		t.Fatalf("unexpected tool call split from accessor")
	}
	if len(r.StaticToolResults()) != 1 || len(r.DynamicToolResults()) != 1 {
		t.Fatalf("unexpected tool result split from accessor")
	}
	if r.RawFinishReason() != "length" {
		t.Fatalf("raw finish reason = %q", r.RawFinishReason())
	}
	if r.ResponseHeadersMap()["x-request-id"] != "stream-1" {
		t.Fatalf("unexpected response headers map: %+v", r.ResponseHeadersMap())
	}
	if r.Request().Body == nil || r.Response().ID != "resp-stream-1" {
		t.Fatalf("unexpected request/response accessors: request=%+v response=%+v", r.Request(), r.Response())
	}
}
