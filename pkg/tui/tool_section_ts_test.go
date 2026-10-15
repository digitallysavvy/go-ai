package tui

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prints the tool input as JSON.stringify(part.input, null, 2): the key
// order the model sent, not a sorted order.
func TestToolInputKeepsModelKeyOrder(t *testing.T) {
	call := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "c1", ToolName: "t",
		Arguments:    map[string]interface{}{"zeta": 1.0, "alpha": map[string]interface{}{"y": "a", "b": []interface{}{1.0, 2.0}}},
		RawArguments: `{"zeta":1,"alpha":{"y":"a","b":[1,2]}}`,
	}}
	s := toolSections(t, call)
	want := "Input:\n{\n  \"zeta\": 1,\n  \"alpha\": {\n    \"y\": \"a\",\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}"
	if s[0].content != want {
		t.Fatalf("content = %q\nwant      %q", s[0].content, want)
	}

	// A raw text that no longer matches the (repaired) input is not used.
	call.ToolCall.Arguments = map[string]interface{}{"other": true}
	if s = toolSections(t, call); s[0].content != "Input:\n{\n  \"other\": true\n}" {
		t.Fatalf("content = %q", s[0].content)
	}
}

// While the input streams, TS shows the repaired partial JSON parsed so far
// (parsePartialJson), and "(streaming...)" until something parses.
func TestToolInputStreamingShowsPartialJSON(t *testing.T) {
	start := provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t"}}
	delta := func(text string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, Text: text, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t"}}
	}
	if s := toolSections(t, start); s[0].content != "Input: (streaming...)" {
		t.Fatalf("content = %q", s[0].content)
	}
	s := toolSections(t, start, delta(`{"b":1,"a":"x`))
	if want := "Input:\n{\n  \"b\": 1,\n  \"a\": \"x\"\n}"; s[0].content != want {
		t.Fatalf("content = %q, want %q", s[0].content, want)
	}
}

// TS renders a collapsed tool, errors and denials included, as a plain tool
// header with the status on the right.
func TestCollapsedToolErrorIsPlainToolHeader(t *testing.T) {
	call := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t"}}
	failed := provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c1", ToolName: "t", Error: errTest("boom")}}
	section := renderToolSection("r:tool:c1", func() *toolSectionState {
		m := newRenderModel(displayModes{tools: TerminalPartDisplayAutoCollapsed}, "r:")
		m.apply(call)
		m.apply(failed)
		return m.tools["c1"]
	}(), true)
	if section.kind != sectionTool || section.title != "Tool · t" || section.rightTitle != "done" || !section.collapsed || section.content != "" {
		t.Fatalf("section = %#v", section)
	}
	full := toolSections(t, call, failed)
	if full[0].kind != sectionError || !strings.HasPrefix(full[0].title, "Tool Error") {
		t.Fatalf("expanded section = %#v", full[0])
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// TS sizes the title border with String.length (UTF-16 units), not the
// display width.
func TestSectionTitleBorderUsesUTF16Length(t *testing.T) {
	// "日本" is two UTF-16 units but four columns wide.
	lines := renderSectionLines(chatSection{kind: sectionTool, title: "日本", content: "x"}, 30)
	top := lines[0]
	// " 日本 " is 4 UTF-16 units.
	if got := strings.Count(top, "─"); got != 30-2-4 {
		t.Fatalf("border cells = %d in %q", got, top)
	}
}
