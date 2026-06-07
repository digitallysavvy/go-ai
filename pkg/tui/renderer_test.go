package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestTerminalRendererStatsAndContextSize(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	inputTokens, outputTokens, totalTokens := int64(3), int64(12), int64(15)
	tps := 12.25
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "# Hello\n- there"},
			{Type: provider.ChunkTypeFinish, Usage: &types.Usage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}},
		}),
		nil,
		types.StepResult{Usage: types.Usage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}, Performance: types.StepPerformance{OutputTokensPerSecond: &tps}},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, ContextSize: 60, Columns: 40, Rows: 20})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{Title: "Test", WaitForExit: false})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}

	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "15 tokens 25%") {
		t.Fatalf("missing context stat in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "12.3 tok/s") {
		t.Fatalf("missing tok/s stat in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "█ Hello") || !strings.Contains(rendered, "• there") {
		t.Fatalf("missing markdown output in:\n%s", rendered)
	}
}

func TestTerminalRendererReadsPromptWithKeyEditing(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("hey\x7fllo\r"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     40,
		Rows:        12,
	})

	prompt, ok, err := renderer.ReadPrompt(context.Background(), TerminalSessionOptions{Title: "Test"})
	if err != nil {
		t.Fatalf("ReadPrompt error: %v", err)
	}
	if !ok {
		t.Fatal("ReadPrompt returned ok=false")
	}
	if prompt != "hello" {
		t.Fatalf("prompt = %q, want hello", prompt)
	}
	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "User") || !strings.Contains(rendered, "hello") {
		t.Fatalf("missing submitted user section:\n%s", rendered)
	}
}

func TestTerminalRendererPromptInterruptsOnCtrlC(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("\u0003"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     40,
		Rows:        12,
	})

	_, _, err := renderer.ReadPrompt(context.Background(), TerminalSessionOptions{Title: "Test"})
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("ReadPrompt error = %v, want Interrupted", err)
	}
}

func TestTerminalRendererReadsToolApprovalDecisionByKey(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("y"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     48,
		Rows:        12,
	})

	response, err := renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{ToolName: "shell"}, TerminalSessionOptions{Title: "Test"})
	if err != nil {
		t.Fatalf("ReadToolApproval error: %v", err)
	}
	if !response.Approved {
		t.Fatalf("approval response = %#v, want approved", response)
	}
	if !strings.Contains(stripANSI(frame.LastFrame()), "Approved") {
		t.Fatalf("missing approved status:\n%s", stripANSI(frame.LastFrame()))
	}
}

func TestTerminalRendererRendersEmptySubmittedPromptWhenPresent(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Columns: 56, Rows: 16})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{
		SubmittedPrompt:    "",
		SubmittedPromptSet: true,
		WaitForExit:        false,
		WaitForExitSet:     true,
	})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "╭ User ") {
		t.Fatalf("missing empty submitted user section:\n%s", rendered)
	}
}

func TestTerminalRendererRendersDeniedToolAsErrorCard(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	reason := "Denied by user."
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "shell", Arguments: map[string]interface{}{"command": "date"}}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID:     "call-1",
				ToolName:       "shell",
				Input:          map[string]interface{}{"command": "date"},
				ApprovalStatus: types.ToolApprovalStatusDenied,
				ApprovalReason: &reason,
			}},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 72, Rows: 20})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{
		WaitForExit:    false,
		WaitForExitSet: true,
	})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "Tool Denied · shell") || !strings.Contains(rendered, "denied") || !strings.Contains(rendered, "Reason: Denied by user.") {
		t.Fatalf("missing denied tool error card:\n%s", rendered)
	}
}

func TestTerminalRendererRendersPendingToolApprovalStatus(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "shell", Arguments: map[string]interface{}{"command": "date"}}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID:     "call-1",
				ToolName:       "shell",
				Input:          map[string]interface{}{"command": "date"},
				ApprovalStatus: types.ToolApprovalStatusUserApproval,
			}},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 72, Rows: 20})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{
		WaitForExit:    false,
		WaitForExitSet: true,
	})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "Tool · shell") || !strings.Contains(rendered, "approval requested") {
		t.Fatalf("missing approval requested status:\n%s", rendered)
	}
	if strings.Contains(rendered, "Output:") {
		t.Fatalf("pending approval should not render an output block:\n%s", rendered)
	}
}

func TestTerminalRendererRendersProviderExecutedApprovedPendingAsExecuting(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "server_tool", Arguments: map[string]interface{}{"query": "x"}, ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID:       "call-1",
				ToolName:         "server_tool",
				Input:            map[string]interface{}{"query": "x"},
				ApprovalStatus:   types.ToolApprovalStatusApproved,
				ProviderExecuted: true,
			}},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 72, Rows: 20})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{
		WaitForExit:    false,
		WaitForExitSet: true,
	})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "Tool · server_tool") || !strings.Contains(rendered, "executing") {
		t.Fatalf("missing provider-executed executing status:\n%s", rendered)
	}
	if strings.Contains(rendered, "Output:") {
		t.Fatalf("provider-executed pending tool should not render an output block:\n%s", rendered)
	}
}

func TestTerminalRendererPreservesTitleWhenOmitted(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("hello\r"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     48,
		Rows:        12,
	})

	_, ok, err := renderer.ReadPrompt(context.Background(), TerminalSessionOptions{Title: "Pinned", TitleSet: true})
	if err != nil || !ok {
		t.Fatalf("ReadPrompt ok=%v err=%v", ok, err)
	}

	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "response"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	_, err = renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{
		WaitForExit:    false,
		WaitForExitSet: true,
	})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	if !strings.Contains(stripANSI(frame.LastFrame()), "Pinned") {
		t.Fatalf("title was not preserved:\n%s", stripANSI(frame.LastFrame()))
	}
}

func TestTerminalRendererScopesSectionsPerSubmittedPrompt(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Columns: 72, Rows: 20})

	first := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "first answer"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: first}, TerminalSessionOptions{
		SubmittedPrompt:    "first",
		SubmittedPromptSet: true,
		ContinueSession:    true,
		WaitForExit:        false,
		WaitForExitSet:     true,
	})
	if err != nil {
		t.Fatalf("first RenderStream error: %v", err)
	}

	second := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "second answer"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	_, err = renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: second}, TerminalSessionOptions{
		SubmittedPrompt:    "second",
		SubmittedPromptSet: true,
		ContinueSession:    true,
		WaitForExit:        false,
		WaitForExitSet:     true,
	})
	if err != nil {
		t.Fatalf("second RenderStream error: %v", err)
	}

	rendered := stripANSI(frame.LastFrame())
	if !strings.Contains(rendered, "first answer") || !strings.Contains(rendered, "second answer") {
		t.Fatalf("normal turns should keep distinct assistant sections:\n%s", rendered)
	}
}

func TestTerminalRendererContinuationRemovesStaleAssistantSections(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 96, Rows: 28})

	first := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "shell", Arguments: map[string]interface{}{"command": "date"}}},
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "waiting for approval"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: first}, TerminalSessionOptions{
		SubmittedPrompt:    "run date",
		SubmittedPromptSet: true,
		ContinueSession:    true,
		WaitForExit:        false,
		WaitForExitSet:     true,
	})
	if err != nil {
		t.Fatalf("first RenderStream error: %v", err)
	}
	if rendered := stripANSI(frame.LastFrame()); !strings.Contains(rendered, "Tool · shell") || !strings.Contains(rendered, "waiting for approval") {
		t.Fatalf("setup did not render tool and text:\n%s", rendered)
	}

	continued := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "approved result"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	_, err = renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: continued}, TerminalSessionOptions{
		ContinueSession: true,
		WaitForExit:     false,
		WaitForExitSet:  true,
	})
	if err != nil {
		t.Fatalf("continued RenderStream error: %v", err)
	}

	rendered := stripANSI(frame.LastFrame())
	if strings.Contains(rendered, "Tool · shell") || strings.Contains(rendered, "waiting for approval") {
		t.Fatalf("continuation kept stale assistant sections:\n%s", rendered)
	}
	if !strings.Contains(rendered, "approved result") {
		t.Fatalf("continuation did not render replacement text:\n%s", rendered)
	}
}

func TestTerminalRendererDisplayModes(t *testing.T) {
	rendered := renderMixedStream(t, TerminalPartDisplayFull, TerminalPartDisplayFull)
	if !strings.Contains(rendered, "Reasoning") || !strings.Contains(rendered, "thinking") {
		t.Fatalf("full reasoning missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Tool · weather") || !strings.Contains(rendered, "Output:") {
		t.Fatalf("full tool missing:\n%s", rendered)
	}

	rendered = renderMixedStream(t, TerminalPartDisplayHidden, TerminalPartDisplayHidden)
	if strings.Contains(rendered, "Reasoning") || strings.Contains(rendered, "Tool · weather") {
		t.Fatalf("hidden sections rendered:\n%s", rendered)
	}

	rendered = renderMixedStream(t, TerminalPartDisplayCollapsed, TerminalPartDisplayCollapsed)
	if !strings.Contains(rendered, "Reasoning") || !strings.Contains(rendered, "Tool · weather") {
		t.Fatalf("collapsed headers missing:\n%s", rendered)
	}
	if strings.Contains(rendered, "thinking") || strings.Contains(rendered, "Output:") {
		t.Fatalf("collapsed content rendered:\n%s", rendered)
	}
}

func TestTerminalRendererAutoCollapsed(t *testing.T) {
	rendered := renderMixedStream(t, TerminalPartDisplayAutoCollapsed, TerminalPartDisplayAutoCollapsed)
	if !strings.Contains(rendered, "Reasoning") || !strings.Contains(rendered, "Tool · weather") || !strings.Contains(rendered, "hello") {
		t.Fatalf("sections missing:\n%s", rendered)
	}
	if strings.Contains(rendered, "thinking") || strings.Contains(rendered, "Output:") {
		t.Fatalf("previous sections were not auto-collapsed:\n%s", rendered)
	}
}

func TestTerminalRendererAutoCollapsedIgnoresHiddenLaterParts(t *testing.T) {
	rendered := renderToolThenReasoningStream(t, TerminalPartDisplayAutoCollapsed, TerminalPartDisplayHidden)
	if !strings.Contains(rendered, "Tool · weather") || !strings.Contains(rendered, "Output:") {
		t.Fatalf("tool should remain expanded when later reasoning is hidden:\n%s", rendered)
	}
	if strings.Contains(rendered, "Reasoning") || strings.Contains(rendered, "thinking") {
		t.Fatalf("hidden reasoning rendered:\n%s", rendered)
	}

	rendered = renderReasoningThenToolStream(t, TerminalPartDisplayHidden, TerminalPartDisplayAutoCollapsed)
	if !strings.Contains(rendered, "Reasoning") || !strings.Contains(rendered, "thinking") {
		t.Fatalf("reasoning should remain expanded when later tool is hidden:\n%s", rendered)
	}
	if strings.Contains(rendered, "Tool · weather") || strings.Contains(rendered, "Output:") {
		t.Fatalf("hidden tool rendered:\n%s", rendered)
	}
}

func TestTerminalRendererDoesNotRenderEmptyReasoningParts(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-1"},
		{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-1"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "Hello"},
		{Type: provider.ChunkTypeFinish},
	}
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Reasoning: TerminalPartDisplayFull, Columns: 72, Rows: 20})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	rendered := stripANSI(frame.LastFrame())
	if strings.Contains(rendered, "Reasoning") {
		t.Fatalf("empty reasoning part rendered:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Hello") {
		t.Fatalf("text part missing:\n%s", rendered)
	}
}

func TestTerminalRendererInterruptsStreamingWhileWaitingForNextChunk(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	input := newTestKeyInput()
	stream := newBlockingTextStream()
	source := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Input: input, Output: out, FrameBuffer: frame, Columns: 48, Rows: 12})
	done := make(chan error, 1)

	go func() {
		_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{Title: "Test", WaitForExit: false})
		done <- err
	}()

	select {
	case <-stream.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	input.Push(TerminalKey{Type: TerminalKeyCtrlC})

	select {
	case err := <-done:
		if !errors.Is(err, errInterrupted) {
			t.Fatalf("RenderStream error = %v, want Interrupted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RenderStream did not interrupt")
	}
	if !stream.closed {
		t.Fatal("stream was not closed after interrupt")
	}
	if !strings.Contains(stripANSI(frame.LastFrame()), "Interrupted") {
		t.Fatalf("missing interrupted status:\n%s", stripANSI(frame.LastFrame()))
	}
}

func TestTerminalRendererCtrlCAtExitPromptDoesNotInterruptCompletedStream(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	input := newTestKeyInput()
	source := NewStreamRenderSourceFromTextStream(
		testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "complete"},
			{Type: provider.ChunkTypeFinish},
		}),
		nil,
		types.StepResult{},
	)
	renderer := NewTerminalRenderer(TerminalRendererOptions{Input: input, Output: out, FrameBuffer: frame, Columns: 56, Rows: 16})
	done := make(chan error, 1)

	go func() {
		_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{})
		done <- err
	}()

	deadline := time.After(time.Second)
	for {
		if strings.Contains(stripANSI(frame.LastFrame()), "Done") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("renderer did not reach done state:\n%s", stripANSI(frame.LastFrame()))
		case <-time.After(5 * time.Millisecond):
		}
	}
	input.Push(TerminalKey{Type: TerminalKeyCtrlC})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RenderStream error = %v, want nil after completed-stream Ctrl+C", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RenderStream did not exit after completed-stream Ctrl+C")
	}
	if strings.Contains(stripANSI(frame.LastFrame()), "Interrupted") {
		t.Fatalf("completed stream was repainted as interrupted:\n%s", stripANSI(frame.LastFrame()))
	}
}

func renderMixedStream(t *testing.T, toolsMode, reasoningMode TerminalPartDisplayMode) string {
	t.Helper()
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "weather", Input: map[string]interface{}{"city": "SF"}, Result: map[string]interface{}{"weather": "sunny"}}},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	}
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: toolsMode, Reasoning: reasoningMode, Columns: 72, Rows: 24})
	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	return stripANSI(frame.LastFrame())
}

func renderToolThenReasoningStream(t *testing.T, toolsMode, reasoningMode TerminalPartDisplayMode) string {
	t.Helper()
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "weather", Input: map[string]interface{}{"city": "SF"}, Result: map[string]interface{}{"weather": "sunny"}}},
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeFinish},
	}
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: toolsMode, Reasoning: reasoningMode, Columns: 72, Rows: 24})
	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	return stripANSI(frame.LastFrame())
}

func renderReasoningThenToolStream(t *testing.T, toolsMode, reasoningMode TerminalPartDisplayMode) string {
	t.Helper()
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "weather", Input: map[string]interface{}{"city": "SF"}, Result: map[string]interface{}{"weather": "sunny"}}},
		{Type: provider.ChunkTypeFinish},
	}
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: toolsMode, Reasoning: reasoningMode, Columns: 72, Rows: 24})
	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	return stripANSI(frame.LastFrame())
}

type testKeyInput struct {
	keys chan TerminalKey
}

func newTestKeyInput() *testKeyInput {
	return &testKeyInput{keys: make(chan TerminalKey, 8)}
}

func (i *testKeyInput) Push(key TerminalKey) {
	i.keys <- key
}

func (i *testKeyInput) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (i *testKeyInput) ReadTerminalKey(ctx context.Context) (TerminalKey, error) {
	select {
	case <-ctx.Done():
		return TerminalKey{}, ctx.Err()
	case key := <-i.keys:
		return key, nil
	}
}

type blockingTextStream struct {
	started chan struct{}
	closed  bool
	once    sync.Once
	mu      sync.Mutex
}

func newBlockingTextStream() *blockingTextStream {
	return &blockingTextStream{started: make(chan struct{})}
}

func (s *blockingTextStream) Next() (*provider.StreamChunk, error) {
	s.once.Do(func() { close(s.started) })
	for {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return nil, io.EOF
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *blockingTextStream) Err() error {
	return nil
}

func (s *blockingTextStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
