package tui

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func toolSections(t *testing.T, chunks ...provider.StreamChunk) []chatSection {
	t.Helper()
	model := newRenderModel(displayModes{tools: TerminalPartDisplayFull, reasoning: TerminalPartDisplayFull}, "r:")
	for _, c := range chunks {
		model.apply(c)
	}
	return model.sections()
}

func TestToolApprovalChunksFromStreamText(t *testing.T) {
	call := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}}}
	request := provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"}}

	s := toolSections(t, call, request)
	if len(s) != 1 || s[0].rightTitle != "approval requested" || s[0].kind != sectionTool {
		t.Fatalf("approval requested: %#v", s)
	}

	approved := provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: true}}
	if s = toolSections(t, call, request, approved); s[0].rightTitle != "executing" {
		t.Fatalf("approved: %#v", s)
	}

	denied := provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: false, Reason: "no thanks"}}
	if s = toolSections(t, call, request, denied); s[0].kind != sectionTool || s[0].rightTitle != "denied" {
		t.Fatalf("denied response: %#v", s)
	}
	outputDenied := provider.StreamChunk{Type: provider.ChunkTypeToolOutputDenied, ToolResult: &types.ToolResult{ToolCallID: "c1"}}
	s = toolSections(t, call, request, denied, outputDenied)
	if s[0].kind != sectionError || !strings.HasPrefix(s[0].title, "Tool Denied") || !strings.HasSuffix(s[0].content, "Reason: no thanks") {
		t.Fatalf("output denied: %#v", s)
	}
}

func TestToolSectionOutputMatchesJSONStringify(t *testing.T) {
	call := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{"q": "a<b&c>"}}}
	result := provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c1", ToolName: "t", Input: map[string]interface{}{"q": "a<b&c>"}, Result: map[string]interface{}{"html": "<b>&</b>"}}}
	s := toolSections(t, call, result)
	if strings.Contains(s[0].content, "\\"+"u003c") || !strings.Contains(s[0].content, `"html": "<b>&</b>"`) || !strings.Contains(s[0].content, `"q": "a<b&c>"`) {
		t.Fatalf("content = %q", s[0].content)
	}

	// A tool that returned null still prints its output (TS output-available).
	nilResult := provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c1", ToolName: "t"}}
	s = toolSections(t, call, nilResult)
	if !strings.HasSuffix(s[0].content, "Output:\nnull") {
		t.Fatalf("content = %q", s[0].content)
	}
}

func TestFormatFloatMatchesToLocaleString(t *testing.T) {
	cases := map[float64]string{12.5: "12.5", 12.04: "12", 1234.56: "1,234.6", 1234: "1,234", 0.04: "0", 0.25: "0.3", 99.95: "100", -3.45: "-3.5"}
	for in, want := range cases {
		if got := formatFloat(in); got != want {
			t.Errorf("formatFloat(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestObserveStatusFollowsTSStatusForStreamChunk(t *testing.T) {
	r := NewTerminalRenderer(TerminalRendererOptions{Output: &recordingWriter{}, Columns: 40, Rows: 10})
	step := func(c provider.ChunkType, tr *types.ToolResult) string {
		r.observeStatus(&provider.StreamChunk{Type: c, ToolResult: tr})
		return r.status
	}
	if got := step(provider.ChunkTypeStartStep, nil); got != processingStatus {
		t.Fatalf("first step: %q", got)
	}
	if got := step(provider.ChunkTypeToolCall, nil); got != executingToolsStatus {
		t.Fatalf("tool call: %q", got)
	}
	if got := step(provider.ChunkTypeToolOutputDenied, &types.ToolResult{}); got != processingToolResultsStatus {
		t.Fatalf("denied: %q", got)
	}
	if got := step(provider.ChunkTypeStartStep, nil); got != processingToolResultsStatus {
		t.Fatalf("step after tool output: %q", got)
	}
	if got := step(provider.ChunkTypeText, nil); got != streamingStatus {
		t.Fatalf("text: %q", got)
	}
	if got := step(provider.ChunkTypeStartStep, nil); got != processingStatus {
		t.Fatalf("step without pending results: %q", got)
	}
	// A result that only asks for approval is not a tool output.
	if got := step(provider.ChunkTypeToolResult, &types.ToolResult{ApprovalStatus: types.ToolApprovalStatusUserApproval}); got != processingStatus {
		t.Fatalf("approval-only result changed status: %q", got)
	}
}

func TestMarkdownListWithUnicodeSpaces(t *testing.T) {
	if got := RenderMarkdown("- item"); got != "• item" {
		t.Fatalf("got %q", got)
	}
	if got := RenderMarkdown("　- item"); got != "　• item" {
		t.Fatalf("got %q", got)
	}
}

func TestJSTrimMatchesJavaScript(t *testing.T) {
	if jsTrim(""+string(rune(0xfeff))+" x  ") != "x" || jsTrim("\u0085x\u0085") != "\u0085x\u0085" {
		t.Fatal("jsTrim differs from String.prototype.trim")
	}
}

func TestVisibleLengthSurvivesInvalidUTF8(t *testing.T) {
	if got := sliceVisible("a\xffb", 3); got != "a�b" {
		t.Fatalf("got %q", got)
	}
	if got := visibleLength("a\xff\xffb"); got != 4 {
		t.Fatalf("got %d", got)
	}
}

func TestEmptySectionShowsPlaceholderOnlyWhenContentEmpty(t *testing.T) {
	lines := renderSectionLines(chatSection{kind: sectionAssistant, title: "A", content: ""}, 30)
	if !strings.Contains(strings.Join(lines, "\n"), "(streaming...)") {
		t.Fatal("no placeholder for empty content")
	}
}
