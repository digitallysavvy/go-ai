package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

// TS workflow-agent-stream-error.test.ts: a terminal error part resolves the
// stream with the partial assistant message kept in result and step messages,
// onEnd carries them and the text, and onError fires once.
func TestWorkflowStreamErrorPartKeepsPartialAssistant(t *testing.T) {
	boom := errors.New("terminal model error")
	m := &fnModel{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "r"},
		{Type: provider.ChunkTypeReasoning, ID: "r", Text: "Thinking."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "r"},
		{Type: provider.ChunkTypeTextStart, ID: "t"},
		{Type: provider.ChunkTypeText, ID: "t", Text: "A partial answer."},
		{Type: provider.ChunkTypeTextEnd, ID: "t"},
		{Type: provider.ChunkTypeError, Err: boom},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError},
	}}
	var mu sync.Mutex
	var endEv *ai.OnFinishEvent
	var errs []error
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m})
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: []types.Message{userMsg("go")},
		OnEnd:    func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); endEv = &e; mu.Unlock() },
		OnError:  func(_ context.Context, e error) { mu.Lock(); errs = append(errs, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ConsumeStream()
	if !errors.Is(st.Err(), boom) || st.FinishReason() != types.FinishReasonError {
		t.Fatalf("err=%v finish=%v", st.Err(), st.FinishReason())
	}
	steps := st.Steps()
	if len(steps) != 1 || len(steps[0].Response.Messages) != 1 {
		t.Fatalf("steps = %+v", steps)
	}
	msg := steps[0].Response.Messages[0]
	if msg.Role != types.RoleAssistant || len(msg.Content) != 2 {
		t.Fatalf("message = %+v", msg)
	}
	if r, ok := msg.Content[0].(types.ReasoningContent); !ok || r.Text != "Thinking." {
		t.Fatalf("reasoning part = %+v", msg.Content[0])
	}
	if p, ok := msg.Content[1].(types.TextContent); !ok || p.Text != "A partial answer." {
		t.Fatalf("text part = %+v", msg.Content[1])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("onError calls = %v", errs)
	}
	if endEv == nil || endEv.Text != "A partial answer." || len(endEv.Messages) != 2 {
		t.Fatalf("onEnd = %+v", endEv)
	}
}

// TS workflow-agent-file-source-retention.test.ts: model files and sources are
// kept in step content, step files/sources, and the assistant message history.
func TestWorkflowStreamRetainsModelFilesAndSources(t *testing.T) {
	src := types.SourceContent{SourceType: "url", ID: "source-1", URL: "https://example.com/source", Title: "Example source"}
	file := types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("file-content")}
	m := &fnModel{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeSource, SourceContent: &src},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &file},
		{Type: provider.ChunkTypeTextStart, ID: "t"},
		{Type: provider.ChunkTypeText, ID: "t", Text: "answer"},
		{Type: provider.ChunkTypeTextEnd, ID: "t"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}}
	var mu sync.Mutex
	var stepEv *ai.OnStepFinishEvent
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m})
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages:  []types.Message{userMsg("q")},
		OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) { mu.Lock(); stepEv = &e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ConsumeStream()
	step := st.Steps()[0]
	if len(step.Files) != 1 || string(step.Files[0].Data) != "file-content" {
		t.Fatalf("step.Files = %+v", step.Files)
	}
	if len(step.Sources) != 1 || step.Sources[0].ID != "source-1" {
		t.Fatalf("step.Sources = %+v", step.Sources)
	}
	if len(step.Content) != 3 {
		t.Fatalf("step.Content = %+v", step.Content)
	}
	mu.Lock()
	defer mu.Unlock()
	if stepEv == nil || len(stepEv.Files) != 1 || len(stepEv.Sources) != 1 {
		t.Fatalf("onStepEnd = %+v", stepEv)
	}
	msg := step.Response.Messages[0]
	if len(msg.Content) != 2 {
		t.Fatalf("assistant message = %+v", msg)
	}
}

// toolLoopModel asks for a tool call for the first n steps, then answers.
type toolLoopModel struct {
	wfMockModel
	n     int
	calls int
}

func (m *toolLoopModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	if m.calls <= m.n {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c", ToolName: "t1", Arguments: map[string]interface{}{}}}}, nil
	}
	return &types.GenerateResult{Text: "Done.", FinishReason: types.FinishReasonStop}, nil
}

func (m *toolLoopModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	m.calls++
	if m.calls <= m.n {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t1", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "Done."},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

// TS workflow-agent-contract.test.ts: only generate() defaults to
// isStepCount(20); stream() is unbounded unless stopWhen is set.
func TestWorkflowDefaultStopWhenAppliesToGenerateNotStream(t *testing.T) {
	tool := types.Tool{Name: "t1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	newAgent := func(n int) *WorkflowAgent {
		a, err := NewWorkflowAgent(WorkflowAgent{Model: &toolLoopModel{n: n}, Tools: []types.Tool{tool}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	gen, err := newAgent(21).GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gen.Steps) != 20 {
		t.Fatalf("generate steps = %d, want 20", len(gen.Steps))
	}
	st, err := newAgent(21).StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ConsumeStream()
	steps := st.Steps()
	if len(steps) != 22 || steps[len(steps)-1].Text != "Done." {
		t.Fatalf("stream steps = %d, want 22 ending in Done.", len(steps))
	}
	st, err = newAgent(21).StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", StopWhen: []ai.StopCondition{ai.IsStepCount(2)}})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ConsumeStream()
	if n := len(st.Steps()); n != 2 {
		t.Fatalf("explicit stream limit steps = %d, want 2", n)
	}
}
