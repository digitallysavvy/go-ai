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

// stepResponseMessages collects every step's response messages.
func stepResponseMessages(steps []types.StepResult) []types.Message {
	var msgs []types.Message
	for _, s := range steps {
		msgs = append(msgs, s.Response.Messages...)
	}
	return msgs
}

// TS WorkflowAgent: a provider-executed tool call that got no result is
// answered with an empty text result (resolveProviderToolResult), and the
// run ends there: modelCallIterator is done after a step without client
// tool calls or pending deferred results.
func TestWorkflowGenerateEndsAfterProviderToolWithoutResult(t *testing.T) {
	m := &providerToolModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(r.Steps) != 1 {
		t.Fatalf("calls=%d steps=%d, want 1 and 1", m.calls, len(r.Steps))
	}
	if !hasEmptyProviderResult(stepResponseMessages(r.Steps)) {
		t.Fatalf("step response lacks the empty provider result: %#v", r.Steps[0].Response.Messages)
	}
}

func TestWorkflowStreamEndsAfterProviderToolWithoutResult(t *testing.T) {
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
	if m.calls != 1 || len(st.Steps()) != 1 {
		t.Fatalf("calls=%d steps=%d, want 1 and 1", m.calls, len(st.Steps()))
	}
	if !hasEmptyProviderResult(stepResponseMessages(st.Steps())) {
		t.Fatalf("step response lacks the empty provider result: %#v", st.Steps()[0].Response.Messages)
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

// The core loops honor the internal option directly (generate and stream):
// the call is answered and the loop ends after the step.
func TestCoreInternalContinueOnMissingProviderResult(t *testing.T) {
	internal := &ai.InternalOptions{ContinueOnMissingProviderResult: true}
	tools := []types.Tool{{Name: "web", ProviderExecuted: true}}
	m := &providerToolModel{}
	r, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: m, Prompt: "p", Tools: tools, Internal: internal, StopWhen: []ai.StopCondition{ai.IsStepCount(5)}})
	if err != nil || m.calls != 1 || len(r.Steps) != 1 || !hasEmptyProviderResult(stepResponseMessages(r.Steps)) {
		t.Fatalf("generate: calls=%d err=%v steps=%#v", m.calls, err, r)
	}
	m = &providerToolModel{}
	st, err := ai.StreamText(context.Background(), ai.StreamTextOptions{Model: m, Prompt: "p", Tools: tools, Internal: internal, StopWhen: []ai.StopCondition{ai.IsStepCount(5)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil || m.calls != 1 || !hasEmptyProviderResult(stepResponseMessages(st.Steps())) {
		t.Fatalf("stream: calls=%d err=%v steps=%#v", m.calls, err, st.Steps())
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
