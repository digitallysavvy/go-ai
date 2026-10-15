package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// providerToolModel answers its first call with a provider-executed tool call
// and no tool result, then finishes with text.
type providerToolModel struct {
	wfMockModel
	calls  int
	second []types.Message
}

func (m *providerToolModel) first() types.ToolCall {
	return types.ToolCall{ID: "p1", ToolName: "web", Arguments: map[string]interface{}{"q": "x"}, ProviderExecuted: true}
}

func (m *providerToolModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	if m.calls == 1 {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{m.first()}}, nil
	}
	m.second = opts.Prompt.Messages
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *providerToolModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.calls++
	if m.calls == 1 {
		c := m.first()
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &c},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	m.second = opts.Prompt.Messages
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "done"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

func hasEmptyProviderResult(msgs []types.Message) bool {
	for _, m := range msgs {
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok && tr.ToolCallID == "p1" {
				if tr.Output != nil {
					return tr.Output.Type == types.ToolResultOutputText && tr.Output.Value == ""
				}
				return tr.Result == ""
			}
		}
	}
	return false
}

// TS WorkflowAgent: a provider-executed tool call that got no result is
// answered with an empty text result and the loop continues.
func TestWorkflowGenerateContinuesAfterProviderToolWithoutResult(t *testing.T) {
	m := &providerToolModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || r.Text != "done" || len(r.Steps) != 2 {
		t.Fatalf("calls=%d text=%q steps=%d", m.calls, r.Text, len(r.Steps))
	}
	if !hasEmptyProviderResult(m.second) {
		t.Fatalf("second call lacks the empty provider result: %#v", m.second)
	}
}

func TestWorkflowStreamContinuesAfterProviderToolWithoutResult(t *testing.T) {
	m := &providerToolModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || st.Text() != "done" || len(st.Steps()) != 2 {
		t.Fatalf("calls=%d text=%q steps=%d", m.calls, st.Text(), len(st.Steps()))
	}
	if !hasEmptyProviderResult(m.second) {
		t.Fatalf("second call lacks the empty provider result: %#v", m.second)
	}
}

// Plain core generateText/streamText keep TS behavior: the loop stops.
func TestCoreStopsAfterProviderToolWithoutResult(t *testing.T) {
	m := &providerToolModel{}
	r, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: m, Prompt: "p", Tools: []types.Tool{{Name: "web", ProviderExecuted: true}},
		StopWhen: []ai.StopCondition{ai.IsStepCount(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(r.Steps) != 1 {
		t.Fatalf("calls=%d steps=%d", m.calls, len(r.Steps))
	}
}

// TS stream mode parses the last step's text only when it is non-empty (any
// finish reason); an empty tool-calls finish has no output and no error.
type emptyToolCallsStreamModel struct{ wfMockModel }

func (m *emptyToolCallsStreamModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "client", Arguments: map[string]interface{}{}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	}), nil
}

func TestWorkflowStreamSkipsOutputParseForEmptyText(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &emptyToolCallsStreamModel{}, Output: ai.JSONOutput(ai.JSONOutputOptions{}),
		Tools: []types.Tool{{Name: "client"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
	if st.Output() != nil || st.OutputErr() != nil {
		t.Fatalf("output=%v err=%v", st.Output(), st.OutputErr())
	}
}

// A parse failure of non-empty text fails the run, like TS where the stream
// execution rejects with the parse error.
func TestWorkflowStreamOutputParseFailureFailsRun(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &badJSONStreamModel{}, Output: ai.JSONOutput(ai.JSONOutputOptions{})})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err == nil || !strings.Contains(err.Error(), "parse") && !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Fatalf("Err = %v, want a parse error", err)
	}
}

type badJSONStreamModel struct{ wfMockModel }

func (m *badJSONStreamModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "{"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

// The core loops honor the internal option directly (generate and stream).
func TestCoreInternalContinueOnMissingProviderResult(t *testing.T) {
	internal := &ai.InternalOptions{ContinueOnMissingProviderResult: true}
	tools := []types.Tool{{Name: "web", ProviderExecuted: true}}
	m := &providerToolModel{}
	r, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: m, Prompt: "p", Tools: tools, Internal: internal, StopWhen: []ai.StopCondition{ai.IsStepCount(5)}})
	if err != nil || m.calls != 2 || r.Text != "done" || !hasEmptyProviderResult(m.second) {
		t.Fatalf("generate: calls=%d err=%v second=%#v", m.calls, err, m.second)
	}
	m = &providerToolModel{}
	st, err := ai.StreamText(context.Background(), ai.StreamTextOptions{Model: m, Prompt: "p", Tools: tools, Internal: internal, StopWhen: []ai.StopCondition{ai.IsStepCount(5)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil || m.calls != 2 || !hasEmptyProviderResult(m.second) {
		t.Fatalf("stream: calls=%d err=%v second=%#v", m.calls, err, m.second)
	}
}

type lengthProviderToolModel struct{ providerToolModel }

func (m *lengthProviderToolModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	return &types.GenerateResult{FinishReason: types.FinishReasonLength, ToolCalls: []types.ToolCall{m.first()}}, nil
}

// TS only answers the tool calls of a tool-calls or stop step; a length finish
// ends the run.
func TestWorkflowGenerateLengthFinishDoesNotAnswerProviderTool(t *testing.T) {
	m := &lengthProviderToolModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 {
		t.Fatalf("calls = %d, want 1", m.calls)
	}
}
