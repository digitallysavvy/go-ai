package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// securityPayload is harmless clipboard/escape content: output is captured
// in an in-memory buffer and never sent to a real terminal, so running
// these tests is safe. Mirrors TS terminal-renderer-security.test.ts's
// `payload`.
const securityPayload = "\x1b]52;c;dGVzdA==\x07\x1bPtest\x1b\\\x9dtest\x9c\x1b[8m"

// expectSafeTUIOutput mirrors TS's expectSafeOutput: the raw bytes ever
// written to the terminal must never contain the dangerous sequences, and
// must contain their escaped form instead.
func expectSafeTUIOutput(t *testing.T, raw string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x1bP", "\x1b[8m", "\x07", "\x9d", "\x9c"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("raw output must not contain %q:\n%s", bad, raw)
		}
	}
	if !strings.Contains(raw, "\\u001b") {
		t.Fatalf("raw output must contain the escaped form \\u001b:\n%s", raw)
	}
}

// TestTerminalRendererEscapesStreamedTextAndReasoning mirrors TS's
// "escapes every streamed prefix of %s without changing the response":
// control sequences streamed one character at a time (as a provider might
// deliver them split across chunks) must never reach the raw terminal
// output, regardless of where a chunk boundary falls.
func TestTerminalRendererEscapesStreamedTextAndReasoning(t *testing.T) {
	for _, kind := range []provider.ChunkType{provider.ChunkTypeText, provider.ChunkTypeReasoning} {
		t.Run(string(kind), func(t *testing.T) {
			out := &recordingWriter{}
			useSync := false
			frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
			renderer := NewTerminalRenderer(TerminalRendererOptions{
				Output:      out,
				FrameBuffer: frame,
				Tools:       TerminalPartDisplayFull,
				Reasoning:   TerminalPartDisplayFull,
				Columns:     80,
				Rows:        24,
			})

			var chunks []provider.StreamChunk
			for _, r := range securityPayload {
				chunks = append(chunks, provider.StreamChunk{Type: kind, ID: "part-1", Text: string(r), Reasoning: string(r)})
			}
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish})

			source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})

			_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false, WaitForExitSet: true})
			if err != nil {
				t.Fatalf("RenderStream error: %v", err)
			}
			expectSafeTUIOutput(t, out.String())
		})
	}
}

// TestTerminalRendererEscapesToolContent mirrors TS's "escapes $name" table
// for tool name, title, input, output, structured output, error, and
// denial reason — every field the TUI surfaces from a tool call/result.
func TestTerminalRendererEscapesToolContent(t *testing.T) {
	cases := []struct {
		name   string
		call   types.ToolCall
		result types.ToolResult
	}{
		{
			name:   "tool name",
			call:   types.ToolCall{ID: "call-1", ToolName: securityPayload},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: securityPayload, Result: "output"},
		},
		{
			name:   "tool title",
			call:   types.ToolCall{ID: "call-1", ToolName: "test", Title: securityPayload},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: "test", Title: securityPayload, Result: "output"},
		},
		{
			name:   "tool input",
			call:   types.ToolCall{ID: "call-1", ToolName: "test", Arguments: map[string]interface{}{"value": securityPayload}},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: "test", Input: map[string]interface{}{"value": securityPayload}, Result: "output"},
		},
		{
			name:   "tool output",
			call:   types.ToolCall{ID: "call-1", ToolName: "test"},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: "test", Result: securityPayload},
		},
		{
			name:   "structured tool output",
			call:   types.ToolCall{ID: "call-1", ToolName: "test"},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: "test", Result: map[string]interface{}{"value": securityPayload}},
		},
		{
			name:   "tool error",
			call:   types.ToolCall{ID: "call-1", ToolName: "test"},
			result: types.ToolResult{ToolCallID: "call-1", ToolName: "test", Error: errors.New(securityPayload)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &recordingWriter{}
			useSync := false
			frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
			renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 80, Rows: 24})

			source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc.call},
				{Type: provider.ChunkTypeToolResult, ToolResult: &tc.result},
				{Type: provider.ChunkTypeFinish},
			}), nil, types.StepResult{})

			_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false, WaitForExitSet: true})
			if err != nil {
				t.Fatalf("RenderStream error: %v", err)
			}
			expectSafeTUIOutput(t, out.String())
		})
	}
}

// TestTerminalRendererEscapesDenialReason mirrors TS's "denial reason" case.
func TestTerminalRendererEscapesDenialReason(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Tools: TerminalPartDisplayFull, Columns: 80, Rows: 24})

	reason := securityPayload
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test"}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
			ToolCallID:     "call-1",
			ToolName:       "test",
			ApprovalStatus: types.ToolApprovalStatusDenied,
			ApprovalReason: &reason,
		}},
		{Type: provider.ChunkTypeFinish},
	}), nil, types.StepResult{})

	_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false, WaitForExitSet: true})
	if err != nil {
		t.Fatalf("RenderStream error: %v", err)
	}
	expectSafeTUIOutput(t, out.String())
}

// TestTerminalRendererEscapesStreamError mirrors TS's "escapes stream
// errors".
func TestTerminalRendererEscapesStreamError(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Output: out, FrameBuffer: frame, Columns: 80, Rows: 24})

	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeError, Text: securityPayload},
	}), nil, types.StepResult{})

	_, _ = renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{WaitForExit: false, WaitForExitSet: true})
	expectSafeTUIOutput(t, out.String())
}

// TestTerminalRendererEscapesTitleAndPromptDisplayWhilePreservingInput
// mirrors TS's "escapes the title and prompt display while preserving
// submitted input": the displayed title/prompt must be escaped, but the
// value ReadPrompt returns to the caller must be the untouched original
// (sanitization is display-only; ReadPrompt's contract is to return
// exactly what the user submitted).
func TestTerminalRendererEscapesTitleAndPromptDisplayWhilePreservingInput(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	renderer := NewTerminalRenderer(TerminalRendererOptions{
		Input:       strings.NewReader("\r"),
		Output:      out,
		FrameBuffer: frame,
		Columns:     80,
		Rows:        24,
	})

	prompt, ok, err := renderer.ReadPrompt(context.Background(), TerminalSessionOptions{Title: securityPayload, InitialPrompt: securityPayload})
	if err != nil {
		t.Fatalf("ReadPrompt error: %v", err)
	}
	if !ok || prompt != securityPayload {
		t.Fatalf("ReadPrompt() = (%q, %v), want the untouched payload preserved", prompt, ok)
	}
	expectSafeTUIOutput(t, out.String())
}

// TestTerminalRendererEscapesApprovalFields mirrors TS's "escapes approval
// %s" table for toolName and title.
func TestTerminalRendererEscapesApprovalFields(t *testing.T) {
	for _, field := range []string{"toolName", "title"} {
		t.Run(field, func(t *testing.T) {
			out := &recordingWriter{}
			useSync := false
			frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
			renderer := NewTerminalRenderer(TerminalRendererOptions{
				Input:       strings.NewReader("n"),
				Output:      out,
				FrameBuffer: frame,
				Columns:     80,
				Rows:        24,
			})

			request := AgentTUIToolApprovalRequest{ApprovalID: "approval-1", ToolCallID: "call-1", ToolName: "test"}
			if field == "toolName" {
				request.ToolName = securityPayload
			} else {
				request.Title = securityPayload
			}

			response, err := renderer.ReadToolApproval(context.Background(), request, TerminalSessionOptions{Title: "Test"})
			if err != nil {
				t.Fatalf("ReadToolApproval error: %v", err)
			}
			if response.Approved {
				t.Fatalf("expected denial")
			}
			expectSafeTUIOutput(t, out.String())
		})
	}
}
