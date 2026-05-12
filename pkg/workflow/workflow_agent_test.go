package workflow

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type wfMockModel struct {
	calls int
	opts  []*provider.GenerateOptions
}

func (m *wfMockModel) SpecificationVersion() string   { return "v3" }
func (m *wfMockModel) Provider() string               { return "mock" }
func (m *wfMockModel) ModelID() string                { return "mock-model" }
func (m *wfMockModel) SupportsTools() bool            { return true }
func (m *wfMockModel) SupportsStructuredOutput() bool { return true }
func (m *wfMockModel) SupportsImageInput() bool       { return false }
func (m *wfMockModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	m.opts = append(m.opts, opts)
	if m.calls == 1 {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}}}, nil
	}
	return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
}
func (m *wfMockModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	chunks := []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "ok"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}
	return testutil.NewMockTextStream(chunks), nil
}

type wfErrorModel struct{ wfMockModel }

func (m *wfErrorModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, errors.New("boom")
}

type wfJSONModel struct {
	wfMockModel
	opts []*provider.GenerateOptions
}

func (m *wfJSONModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.opts = append(m.opts, opts)
	return &types.GenerateResult{Text: `{"ok":true}`, FinishReason: types.FinishReasonStop}, nil
}

func TestNewWorkflowAgentValidation(t *testing.T) {
	if _, err := NewWorkflowAgent(WorkflowAgent{}); err == nil {
		t.Fatal("expected error for nil model")
	}
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(agent.StopWhen) == 0 {
		t.Fatalf("expected default stop condition")
	}
}

func TestWorkflowPromptMessagesValidation(t *testing.T) {
	agent, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	_, err = agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "x", Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "y"}}}}})
	if err == nil {
		t.Fatal("expected mutually exclusive validation error")
	}
}

func TestWorkflowActiveToolsFiltering(t *testing.T) {
	m := &wfMockModel{}
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model: m,
		Tools: []types.Tool{{Name: "t1"}, {Name: "t2"}},
	})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if len(m.opts) == 0 || len(m.opts[0].Tools) != 2 {
		t.Fatalf("expected both tools, got %+v", m.opts)
	}

	m2 := &wfMockModel{}
	agent2, _ := NewWorkflowAgent(WorkflowAgent{Model: m2, Tools: []types.Tool{{Name: "t1"}, {Name: "t2"}}, ActiveTools: []string{"t1"}})
	_, err = agent2.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if len(m2.opts) == 0 || len(m2.opts[0].Tools) != 1 || m2.opts[0].Tools[0].Name != "t1" {
		t.Fatalf("expected filtered tools, got %+v", m2.opts[0].Tools)
	}
}

func TestWorkflowStreamWithOptions(t *testing.T) {
	agent, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	res, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if res == nil || res.StreamTextResult == nil {
		t.Fatalf("expected stream result")
	}
	for {
		_, e := res.Stream().Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream next err: %v", e)
		}
	}
}

func TestWorkflowInstructionsTelemetryAndOutputForwarding(t *testing.T) {
	model := &wfMockModel{}
	telemetry := &ai.TelemetrySettings{FunctionID: "workflow-test"}
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model:        model,
		Instructions: "use the instructions alias",
		Output:       map[string]interface{}{"type": "object"},
		Telemetry:    telemetry,
	})
	res, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if res == nil || res.Output == nil {
		t.Fatalf("expected workflow result output")
	}
	if len(model.opts) == 0 {
		t.Fatal("expected model call")
	}
	if model.opts[0].Prompt.System != "use the instructions alias" {
		t.Fatalf("instructions not forwarded as system: %q", model.opts[0].Prompt.System)
	}
	if model.opts[0].Telemetry != telemetry {
		t.Fatalf("telemetry was not forwarded")
	}
}

func TestWorkflowStructuredOutputGenerateAndToolSet(t *testing.T) {
	model := &wfJSONModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model:        model,
		Instructions: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "system message"}}}},
		ToolSet:      map[string]types.Tool{"lookup": {Description: "lookup"}},
		Output:       ai.JSONOutput(ai.JSONOutputOptions{}),
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	res, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if model.opts[0].Prompt.System != "system message" {
		t.Fatalf("unexpected system instructions: %q", model.opts[0].Prompt.System)
	}
	if len(model.opts[0].Tools) != 1 || model.opts[0].Tools[0].Name != "lookup" {
		t.Fatalf("tool set was not converted: %+v", model.opts[0].Tools)
	}
	if model.opts[0].ResponseFormat == nil || model.opts[0].ResponseFormat.Type != "json" {
		t.Fatalf("expected JSON response format, got %+v", model.opts[0].ResponseFormat)
	}
	output, ok := res.Output.(map[string]interface{})
	if !ok || output["ok"] != true {
		t.Fatalf("expected parsed JSON output, got %#v", res.Output)
	}
}

func TestWorkflowErrorAndAbortCallbacks(t *testing.T) {
	errCalled := false
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model: &wfErrorModel{},
		OnError: func(context.Context, error) {
			errCalled = true
		},
	})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err == nil || !errCalled {
		t.Fatalf("expected error callback and error, called=%v err=%v", errCalled, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	abortCalled := false
	agent2, _ := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		OnAbort: func(context.Context, []types.StepResult) {
			abortCalled = true
		},
	})
	_, err = agent2.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hello"})
	if err == nil || !abortCalled {
		t.Fatalf("expected abort callback and error, called=%v err=%v", abortCalled, err)
	}
}
