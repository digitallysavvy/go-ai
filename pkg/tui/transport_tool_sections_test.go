package tui

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func transportSections(t *testing.T, chunks ...ai.UIMessageChunk) []chatSection {
	t.Helper()
	meta := map[string]toolCallMeta{}
	approvals := map[string]string{}
	m := newRenderModel(displayModes{tools: TerminalPartDisplayFull, reasoning: TerminalPartDisplayFull, responseStatistics: ResponseStatisticsOutputTokensPerSecond}, "r1")
	for _, c := range chunks {
		if pc, ok := uiMessageChunkToProviderChunk(c, meta, approvals); ok {
			m.apply(*pc)
		}
	}
	return m.sections()
}

// A UI chunk for an approval request or tool output carries no input; the
// tool part keeps the input from tool-input-available (TS part.input).
func TestTransportToolSectionKeepsInputAfterApprovalAndOutput(t *testing.T) {
	input := map[string]interface{}{"city": "London"}
	sections := transportSections(t,
		ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": "c1", "toolName": "weather", "input": input},
		ai.UIMessageChunk{"type": "tool-approval-request", "toolCallId": "c1", "approvalId": "a1"},
	)
	if len(sections) != 1 || !strings.Contains(sections[0].content, `"city": "London"`) {
		t.Fatalf("approval-requested section = %+v, want the input shown", sections)
	}
	sections = transportSections(t,
		ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": "c1", "toolName": "weather", "input": input},
		ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": "c1", "output": "sunny"},
	)
	if len(sections) != 1 || !strings.Contains(sections[0].content, `"city": "London"`) || !strings.Contains(sections[0].content, "Output:\nsunny") {
		t.Fatalf("output section = %+v, want input and output", sections)
	}
}

// TS readUIMessageStream turns tool-input-error into an output-error part, which
// the renderer shows as a Tool Error section with the input and error text.
func TestTransportToolInputErrorRendersToolError(t *testing.T) {
	sections := transportSections(t,
		ai.UIMessageChunk{"type": "tool-input-error", "toolCallId": "c1", "toolName": "weather", "input": map[string]interface{}{"city": 1}, "errorText": "bad input"},
	)
	if len(sections) != 1 || sections[0].kind != sectionError || sections[0].title != "Tool Error · weather" ||
		!strings.Contains(sections[0].content, "Error:\nbad input") || !strings.Contains(sections[0].content, `"city": 1`) {
		t.Fatalf("sections = %+v", sections)
	}
}

// TS statusForStreamChunk has no tool-input-error case: the status line and the
// pending-tool-results flag are untouched.
func TestTransportToolInputErrorLeavesStatusAlone(t *testing.T) {
	r := NewTerminalRenderer(TerminalRendererOptions{Output: &recordingWriter{}, Columns: 40, Rows: 10})
	r.observeStatus(&provider.StreamChunk{Type: provider.ChunkTypeStartStep})
	before := r.status
	pc, ok := uiMessageChunkToProviderChunk(ai.UIMessageChunk{"type": "tool-input-error", "toolCallId": "c1", "toolName": "w", "errorText": "bad"}, map[string]toolCallMeta{}, map[string]string{})
	if !ok {
		t.Fatal("not converted")
	}
	r.observeStatus(pc)
	if r.status != before || r.pendingToolResults {
		t.Fatalf("status = %q pending=%v, want unchanged %q", r.status, r.pendingToolResults, before)
	}
}
