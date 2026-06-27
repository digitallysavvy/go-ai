package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestAgentTUIRunnerPromptsAndStreamsMessages(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &runnerMockRenderer{prompts: []string{"hello"}}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.streamCalls) != 1 {
		t.Fatalf("stream calls = %d, want 1", len(mock.streamCalls))
	}
	if got := textFromMessages(mock.streamCalls[0].Messages); got != "hello" {
		t.Fatalf("stream prompt = %q, want hello", got)
	}
}

func TestAgentTUIRunnerSubmitsEmptyPromptLikeTypeScript(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &runnerMockRenderer{prompts: []string{""}}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.streamCalls) != 1 {
		t.Fatalf("stream calls = %d, want 1", len(mock.streamCalls))
	}
	if got := textFromMessages(mock.streamCalls[0].Messages); got != "" {
		t.Fatalf("stream prompt = %q, want empty", got)
	}
}

func TestAgentTUIRunnerForwardsSandboxToAgentStream(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &runnerMockRenderer{prompts: []string{"hello"}}
	sandbox := struct{ name string }{name: "sandbox"}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
		Sandbox:  sandbox,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(mock.streamCalls) != 1 {
		t.Fatalf("stream calls = %d, want 1", len(mock.streamCalls))
	}
	if mock.streamCalls[0].ExperimentalSandbox != sandbox {
		t.Fatalf("sandbox = %#v, want %#v", mock.streamCalls[0].ExperimentalSandbox, sandbox)
	}
}

func TestAgentTUIRunnerContinuesWithToolApprovalResponse(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &runnerMockRenderer{
		prompts: []string{"run"},
		renderedMessages: [][]types.Message{
			{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ToolApprovalRequestContent{
							ApprovalID: "approval-1",
							ToolCall: types.ToolCall{
								ID:        "call-1",
								ToolName:  "shell",
								Arguments: map[string]interface{}{"command": "date"},
							},
						},
					},
				},
			},
			{
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "done"}}},
			},
		},
		approvalResponses: []AgentTUIToolApprovalResponse{{Approved: true}},
	}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(mock.streamCalls) != 2 {
		t.Fatalf("stream calls = %d, want 2", len(mock.streamCalls))
	}
	second := mock.streamCalls[1].Messages
	if len(second) < 2 {
		t.Fatalf("second call messages = %d, want approval response message", len(second))
	}
	part, ok := second[1].Content[0].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("approval response part type = %T", second[1].Content[0])
	}
	if !part.Approved || part.ApprovalID != "approval-1" {
		t.Fatalf("approval response = %+v", part)
	}
}

func TestAgentTUIRunnerErrorsWhenInitialPromptUnsupported(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &renderOnlyRunnerRenderer{}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
	})

	err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "renderer does not support prompt input") {
		t.Fatalf("Run error = %v, want prompt input error", err)
	}
	if len(mock.streamCalls) != 0 {
		t.Fatalf("stream calls = %d, want 0", len(mock.streamCalls))
	}
}

func TestAgentTUIRunnerPassesContinueSessionFromPromptSupport(t *testing.T) {
	mock := &runnerMockAgent{}
	renderer := &runnerMockRenderer{prompts: []string{"hello"}}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:    mock,
		Renderer: renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(renderer.continueSessions) != 1 || !renderer.continueSessions[0] {
		t.Fatalf("continue sessions = %v, want [true]", renderer.continueSessions)
	}
}

type runnerMockRenderer struct {
	prompts           []string
	index             int
	renderedMessages  [][]types.Message
	renderIndex       int
	approvalResponses []AgentTUIToolApprovalResponse
	approvalIndex     int
	continueSessions  []bool
}

func (r *runnerMockRenderer) ReadPrompt(context.Context, TerminalSessionOptions) (string, bool, error) {
	if r.index >= len(r.prompts) {
		return "", false, nil
	}
	prompt := r.prompts[r.index]
	r.index++
	return prompt, true, nil
}

func (r *runnerMockRenderer) RenderStream(ctx context.Context, result AgentTUIStreamResult, options TerminalSessionOptions) ([]types.Message, error) {
	r.continueSessions = append(r.continueSessions, options.ContinueSession)
	for {
		_, err := result.Stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if r.renderIndex < len(r.renderedMessages) {
		messages := r.renderedMessages[r.renderIndex]
		r.renderIndex++
		return messages, nil
	}
	return []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "ok"}}}}, nil
}

func (r *runnerMockRenderer) ReadToolApproval(context.Context, AgentTUIToolApprovalRequest, TerminalSessionOptions) (AgentTUIToolApprovalResponse, error) {
	if r.approvalIndex >= len(r.approvalResponses) {
		return AgentTUIToolApprovalResponse{Approved: false, Reason: "Denied by user."}, nil
	}
	response := r.approvalResponses[r.approvalIndex]
	r.approvalIndex++
	return response, nil
}

type renderOnlyRunnerRenderer struct {
	continueSessions []bool
}

func (r *renderOnlyRunnerRenderer) RenderStream(ctx context.Context, result AgentTUIStreamResult, options TerminalSessionOptions) ([]types.Message, error) {
	r.continueSessions = append(r.continueSessions, options.ContinueSession)
	for {
		_, err := result.Stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "ok"}}}}, nil
}

type runnerMockAgent struct {
	streamCalls []agent.AgentStreamOptions
}

func (a *runnerMockAgent) Version() string { return "test" }
func (a *runnerMockAgent) ID() string      { return "test" }
func (a *runnerMockAgent) Tools() []types.Tool {
	return nil
}
func (a *runnerMockAgent) Generate(context.Context, agent.AgentGenerateOptions) (*ai.GenerateTextResult, error) {
	return nil, nil
}
func (a *runnerMockAgent) Stream(ctx context.Context, opts agent.AgentStreamOptions) (*ai.StreamTextResult, error) {
	a.streamCalls = append(a.streamCalls, opts)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	return ai.StreamText(ctx, ai.StreamTextOptions{Model: model, Messages: opts.Messages})
}
func (a *runnerMockAgent) Execute(context.Context, string) (*agent.AgentResult, error) {
	return nil, nil
}
func (a *runnerMockAgent) ExecuteWithMessages(context.Context, []types.Message) (*agent.AgentResult, error) {
	return nil, nil
}

func textFromMessages(messages []types.Message) string {
	for _, message := range messages {
		for _, part := range message.Content {
			if text, ok := part.(types.TextContent); ok {
				return text.Text
			}
		}
	}
	return ""
}
