package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// fakeChatTransport is a minimal ai.ChatTransport that plays back a scripted
// sequence of UI message chunks for each SendMessages call, in order. It
// records every request so tests can assert on ChatID/Trigger/Messages.
type fakeChatTransport struct {
	mu        sync.Mutex
	calls     []ai.ChatTransportSendMessagesRequest
	responses [][]ai.UIMessageChunk
}

func (f *fakeChatTransport) SendMessages(ctx context.Context, req ai.ChatTransportSendMessagesRequest) (<-chan ai.UIMessageChunk, <-chan error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	idx := len(f.calls) - 1
	f.mu.Unlock()

	chunks := make(chan ai.UIMessageChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		f.mu.Lock()
		var script []ai.UIMessageChunk
		if idx < len(f.responses) {
			script = f.responses[idx]
		}
		f.mu.Unlock()
		for _, c := range script {
			select {
			case chunks <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return chunks, errs
}

func (f *fakeChatTransport) requests() []ai.ChatTransportSendMessagesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ai.ChatTransportSendMessagesRequest(nil), f.calls...)
}

// TestRunAgentTUIRequiresExactlyOneOfAgentOrTransport mirrors TS
// run-agent-tui.ts's `{ agent } | { transport }` union: RunAgentTUIOptions
// must reject both set and neither set.
func TestRunAgentTUIRequiresExactlyOneOfAgentOrTransport(t *testing.T) {
	t.Run("both set", func(t *testing.T) {
		err := RunAgentTUI(context.Background(), RunAgentTUIOptions{
			Agent:     &runnerMockAgent{},
			Transport: &fakeChatTransport{},
		})
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("err = %v, want mutually exclusive error", err)
		}
	})

	t.Run("neither set", func(t *testing.T) {
		err := RunAgentTUI(context.Background(), RunAgentTUIOptions{})
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("err = %v, want required error", err)
		}
	})
}

// TestAgentTUIRunnerRunRequiresExactlyOneOfAgentOrTransport exercises the
// same validation at the AgentTUIRunner level (used directly by callers that
// bypass RunAgentTUI, e.g. to supply a custom renderer).
func TestAgentTUIRunnerRunRequiresExactlyOneOfAgentOrTransport(t *testing.T) {
	renderer := &runnerMockRenderer{prompts: []string{"hi"}}

	both := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:     &runnerMockAgent{},
		Transport: &fakeChatTransport{},
		Renderer:  renderer,
	})
	if err := both.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want mutually exclusive error", err)
	}

	neither := NewAgentTUIRunner(AgentTUIRunnerOptions{Renderer: renderer})
	if err := neither.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v, want required error", err)
	}
}

// TestAgentTUIRunnerStreamsViaChatTransport drives AgentTUIRunner with a fake
// ChatTransport instead of an agent.Agent and asserts the streamed text
// chunks are rendered, and that SendMessages was called with the expected
// ChatID/Trigger — mirroring TS AgentTUIRunner.streamMessages' transport
// branch (agent-tui-runner.ts), which calls
// `transport.sendMessages({ trigger: 'submit-message', chatId, ... })`.
func TestAgentTUIRunnerStreamsViaChatTransport(t *testing.T) {
	transport := &fakeChatTransport{
		responses: [][]ai.UIMessageChunk{
			{
				{"type": "text-start", "id": "t1"},
				{"type": "text-delta", "id": "t1", "delta": "hello"},
				{"type": "text-delta", "id": "t1", "delta": " world"},
				{"type": "text-end", "id": "t1"},
				{"type": "finish"},
			},
		},
	}
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("hi\r"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     40,
		Rows:        12,
	})

	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Transport: transport,
		ChatID:    "chat-1",
		Renderer:  renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	calls := transport.requests()
	if len(calls) != 1 {
		t.Fatalf("SendMessages calls = %d, want 1", len(calls))
	}
	if calls[0].ChatID != "chat-1" {
		t.Fatalf("ChatID = %q, want chat-1", calls[0].ChatID)
	}
	if calls[0].Trigger != "submit-message" {
		t.Fatalf("Trigger = %q, want submit-message", calls[0].Trigger)
	}
	if got := textFromMessages(calls[0].Messages); got != "hi" {
		t.Fatalf("request prompt = %q, want hi", got)
	}

	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "hello world") {
		t.Fatalf("missing streamed text in:\n%s", rendered)
	}
}

// TestAgentTUIRunnerChatTransportGeneratesChatIDWhenUnset mirrors TS
// AgentTUIRunner's `chatId = generateId()`: when ChatID is left empty, a
// non-empty id is generated and reused across turns.
func TestAgentTUIRunnerChatTransportGeneratesChatIDWhenUnset(t *testing.T) {
	transport := &fakeChatTransport{
		responses: [][]ai.UIMessageChunk{
			{{"type": "text-delta", "id": "t1", "delta": "hi"}, {"type": "finish"}},
		},
	}
	renderer := &runnerMockRenderer{prompts: []string{"hello"}}
	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{Transport: transport, Renderer: renderer})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	calls := transport.requests()
	if len(calls) != 1 || calls[0].ChatID == "" {
		t.Fatalf("calls = %#v, want one call with a generated ChatID", calls)
	}
}

// TestAgentTUIRunnerChatTransportToolApprovalFlow exercises a full
// tool-approval round trip through a ChatTransport: the transport emits a
// pending tool call requiring approval, the (real) TerminalRenderer reads the
// user's "y" keypress, and the runner re-sends the message history —
// including the resulting types.ToolApprovalResponseContent — on the next
// SendMessages call. Mirrors TS AgentTUIRunner.run's approval loop
// (agent-tui-runner.ts), now driven by transport instead of agent.
func TestAgentTUIRunnerChatTransportToolApprovalFlow(t *testing.T) {
	transport := &fakeChatTransport{
		responses: [][]ai.UIMessageChunk{
			{
				{"type": "tool-input-start", "toolCallId": "call-1", "toolName": "shell"},
				{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "shell", "input": map[string]interface{}{"command": "date"}},
				{"type": "tool-approval-request", "toolCallId": "call-1", "approvalId": "approval-1"},
				{"type": "finish"},
			},
			{
				{"type": "text-start", "id": "t2"},
				{"type": "text-delta", "id": "t2", "delta": "done"},
				{"type": "text-end", "id": "t2"},
				{"type": "finish"},
			},
		},
	}
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	// "run\r" submits the first prompt; "y" approves the pending tool call
	// when AgentTUIRunner asks the (shared) renderer for a decision; the
	// reader is then exhausted so the next ReadPrompt call ends the session.
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("run\ry"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     60,
		Rows:        20,
	})

	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Transport: transport,
		ChatID:    "chat-1",
		Renderer:  renderer,
	})

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	calls := transport.requests()
	if len(calls) != 2 {
		t.Fatalf("SendMessages calls = %d, want 2", len(calls))
	}

	second := calls[1].Messages
	var approvalResponse *types.ToolApprovalResponseContent
	for _, message := range second {
		for _, part := range message.Content {
			if response, ok := part.(types.ToolApprovalResponseContent); ok {
				response := response
				approvalResponse = &response
			}
		}
	}
	if approvalResponse == nil {
		t.Fatalf("second request messages missing tool approval response: %#v", second)
	}
	if !approvalResponse.Approved || approvalResponse.ApprovalID != "approval-1" {
		t.Fatalf("approval response = %+v, want approved approval-1", approvalResponse)
	}

	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "done") {
		t.Fatalf("missing final assistant text in:\n%s", rendered)
	}
}
