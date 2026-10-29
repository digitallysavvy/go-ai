package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// wfScriptModel answers each model call with the next scripted response, in
// generate and in stream mode.
type wfScriptModel struct {
	wfMockModel
	responses []types.GenerateResult
}

func (m *wfScriptModel) next() types.GenerateResult {
	i := m.calls
	m.calls++
	if i >= len(m.responses) {
		return types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "extra"}}, FinishReason: types.FinishReasonStop}
	}
	return m.responses[i]
}

func (m *wfScriptModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.opts = append(m.opts, opts)
	r := m.next()
	return &r, nil
}

func (m *wfScriptModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.opts = append(m.opts, opts)
	r := m.next()
	var chunks []provider.StreamChunk
	for _, p := range r.Content {
		switch v := p.(type) {
		case types.TextContent:
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "text"}, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text", Text: v.Text}, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "text"})
		case types.ToolCallContent:
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: v.ToolCallID, ToolName: v.ToolName, RawArguments: v.Input, ProviderExecuted: v.ProviderExecuted}})
		case types.ToolResultContent:
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: v.ToolCallID, ToolName: v.ToolName, Result: v.Result, ProviderExecuted: true}})
		}
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: r.FinishReason})
	return testutil.NewMockTextStream(chunks), nil
}

// TS workflow-agent-approval.test.ts "retains deferred provider results and
// local tool errors", in generate and stream mode: the deferred provider
// result of the second response is one of the run's tool results.
func TestR61WorkflowRetainsDeferredProviderResultsAndLocalToolErrors(t *testing.T) {
	responses := func() []types.GenerateResult {
		first := []types.ContentPart{
			types.ToolCallContent{ToolCallID: "remote-call", ToolName: "remote", Input: `{"value":"requested"}`, ProviderExecuted: true},
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "action", Input: `{"value":"requested"}`},
		}
		r0 := types.GenerateResult{Content: first, FinishReason: types.FinishReasonToolCalls,
			ToolCalls: []types.ToolCall{{ID: "remote-call", ToolName: "remote", RawArguments: `{"value":"requested"}`, ProviderExecuted: true}, {ID: "call-1", ToolName: "action", RawArguments: `{"value":"requested"}`}}}
		r1 := types.GenerateResult{Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "remote-call", ToolName: "remote", Result: map[string]interface{}{"answer": 42}},
			types.TextContent{Text: "Done"},
		}, FinishReason: types.FinishReasonStop}
		return []types.GenerateResult{r0, r1}
	}
	tools := []types.Tool{
		{Name: "remote", ProviderExecuted: true, SupportsDeferredResults: true},
		{Name: "action", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("local failure")
		}},
	}
	check := func(t *testing.T, steps []types.StepResult, text string) {
		t.Helper()
		if text != "Done" {
			t.Fatalf("text = %q", text)
		}
		var sawError, sawRemote bool
		for _, step := range steps {
			for _, p := range step.Content {
				if e, ok := p.(types.ToolErrorContent); ok && e.ToolCallID == "call-1" {
					sawError = true
				}
			}
			for _, r := range step.ToolResults {
				if r.ToolCallID == "remote-call" && r.ProviderExecuted {
					if m, ok := r.Result.(map[string]interface{}); ok && m["answer"] == 42 {
						sawRemote = true
					}
				}
			}
		}
		if !sawError {
			t.Fatalf("no tool-error for call-1 in %+v", steps)
		}
		if !sawRemote {
			t.Fatalf("no provider result for remote-call in the step tool results: %+v", steps)
		}
	}
	t.Run("generate", func(t *testing.T) {
		a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfScriptModel{responses: responses()}, Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "Perform the action."})
		if err != nil {
			t.Fatal(err)
		}
		check(t, res.Steps, res.Text)
	})
	t.Run("stream", func(t *testing.T) {
		a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfScriptModel{responses: responses()}, Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "Perform the action."})
		if err != nil {
			t.Fatal(err)
		}
		if err := res.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		check(t, res.Steps(), res.Text())
	})
}
