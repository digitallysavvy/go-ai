package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// portcarrytuiKeys feeds one key per Read and calls before(i) in the reader's
// goroutine (the renderer's) ahead of key i, so a test can look at the frame
// the previous key produced without racing the renderer. After the last key it
// returns io.EOF (a denial).
type portcarrytuiKeys struct {
	keys   []string
	before func(i int)
	i      int
}

func (k *portcarrytuiKeys) Read(p []byte) (int, error) {
	if k.before != nil {
		k.before(k.i)
	}
	if k.i >= len(k.keys) {
		return 0, io.EOF
	}
	n := copy(p, k.keys[k.i])
	k.i++
	return n, nil
}

type portcarrytuiTerm struct {
	renderer *TerminalRenderer
	frame    *TerminalFrameBuffer
	out      *recordingWriter
}

func portcarrytuiNew(input io.Reader, columns, rows int, tools TerminalPartDisplayMode) portcarrytuiTerm {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	return portcarrytuiTerm{
		renderer: NewTerminalRenderer(TerminalRendererOptions{Input: input, Output: out, FrameBuffer: frame, Columns: columns, Rows: rows, Tools: tools}),
		frame:    frame,
		out:      out,
	}
}

func (tt portcarrytuiTerm) screen() string { return stripANSI(tt.frame.LastFrame()) }

func (tt portcarrytuiTerm) render(t *testing.T, chunks ...provider.StreamChunk) {
	t.Helper()
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish})
	source := NewStreamRenderSourceFromTextStream(testutil.NewMockTextStream(chunks), nil, types.StepResult{})
	if _, err := tt.renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{Title: "Test", WaitForExit: false, WaitForExitSet: true}); err != nil {
		t.Fatalf("RenderStream: %v", err)
	}
}

func portcarrytuiToolCall(id, name string, args map[string]interface{}) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: name, Arguments: args}}
}

func portcarrytuiLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "Line " + formatInt(int64(i+1))
	}
	return strings.Join(lines, "\n\n")
}

// TS 25972d7857 escapeFormatCharacters tests.
func TestPortcarrytuiEscapeFormatCharacters(t *testing.T) {
	got := escapeFormatCharacters("a\u202eb\u2066c\u200bd\u200de\ufefff\U000e0041g")
	want := `a\u{202e}b\u{2066}c\u{200b}d\u{200d}e\u{feff}f\u{e0041}g`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	visible := "Hello 日本語 😀 é ☀️"
	if got := escapeFormatCharacters(visible); got != visible {
		t.Fatalf("visible characters changed: %q", got)
	}
}

// TS "shows the tool input awaiting approval without markdown formatting".
func TestPortcarrytuiApprovalInputWithoutMarkdown(t *testing.T) {
	tt := portcarrytuiNew(&portcarrytuiKeys{keys: []string{"n"}}, 72, 16, "")
	var seen string
	tt.renderer.input = &portcarrytuiKeys{keys: []string{"n"}, before: func(i int) {
		if i == 0 {
			seen = tt.screen()
		}
	}}
	_, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
		ApprovalID: "approval-1", ToolCallID: "call-1", ToolName: "shell",
		Input: map[string]interface{}{"command": "rm *.log *.tmp && echo `whoami` > __status__"},
	}, TerminalSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"╭ Tool · shell ",
		" approve? y/n ╮",
		`"command": "rm *.log *.tmp && echo ` + "`whoami`" + ` > __status__"`,
		"Approve tool shell? y/n · ↑/↓ · PgUp/PgDn · Esc/Ctrl+C",
	} {
		if !strings.Contains(seen, want) {
			t.Fatalf("missing %q in:\n%s", want, seen)
		}
	}
}

// TS "keeps the approval marker visible when the tool title is long" and "...
// has wide characters".
func TestPortcarrytuiApprovalMarkerSurvivesLongTitle(t *testing.T) {
	for name, tc := range map[string]struct{ title, want string }{
		"long": {"Run a shell command in the workspace", "╭ Tool · Run a shel…  approve? y/n ╮"},
		"wide": {"シェルコマンドを実行する", "╭ Tool · シェルコマ…  approve? y/n ╮"},
	} {
		t.Run(name, func(t *testing.T) {
			tt := portcarrytuiNew(nil, 40, 14, "")
			var seen string
			tt.renderer.input = &portcarrytuiKeys{keys: []string{"n"}, before: func(i int) {
				if i == 0 {
					seen = tt.screen()
				}
			}}
			_, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
				ToolCallID: "call-1", ToolName: "shell", Title: tc.title, Input: map[string]interface{}{"command": "date"},
			}, TerminalSessionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(seen, tc.want) {
				t.Fatalf("missing %q in:\n%s", tc.want, seen)
			}
		})
	}
}

// TS "shows the input of the tool call that each approval decision applies to"
// (two calls of the same tool, default auto-collapsed) plus "scrolls the tool
// call awaiting approval into view".
func TestPortcarrytuiApprovalShowsTheCallItAppliesTo(t *testing.T) {
	tt := portcarrytuiNew(nil, 48, 14, "")
	tt.render(t,
		portcarrytuiToolCall("call-1", "shell", map[string]interface{}{"command": "SENSITIVE"}),
		portcarrytuiToolCall("call-2", "shell", map[string]interface{}{"command": "BENIGN"}),
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text-1", Text: portcarrytuiLines(10)},
	)
	if strings.Contains(tt.screen(), "SENSITIVE") {
		t.Fatalf("collapsed call must not show its input before the decision:\n%s", tt.screen())
	}

	var atPrompt, afterFirst string
	tt.renderer.input = &portcarrytuiKeys{keys: []string{"y", "n"}, before: func(i int) {
		switch i {
		case 0:
			atPrompt = tt.screen()
		case 1:
			afterFirst = tt.screen()
		}
	}}
	for _, id := range []string{"call-1", "call-2"} {
		input := "SENSITIVE"
		if id == "call-2" {
			input = "BENIGN"
		}
		if id == "call-2" {
			// The second decision starts with the second call's input.
			tt.renderer.input = &portcarrytuiKeys{keys: []string{"n"}, before: func(i int) {
				if i == 0 {
					afterFirst = tt.screen()
				}
			}}
		}
		resp, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
			ToolCallID: id, ToolName: "shell", Input: map[string]interface{}{"command": input},
		}, TerminalSessionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Approved != (id == "call-1") {
			t.Fatalf("%s approved = %v", id, resp.Approved)
		}
		if id == "call-1" {
			if !strings.Contains(atPrompt, `"command": "SENSITIVE"`) || !strings.Contains(atPrompt, "approve? y/n") || strings.Contains(atPrompt, "BENIGN") {
				t.Fatalf("prompt for call-1 does not show call-1's input:\n%s", atPrompt)
			}
			if got := strings.Count(atPrompt, "approve? y/n"); got != 1 {
				t.Fatalf("approve marker count = %d", got)
			}
		} else if !strings.Contains(afterFirst, `"command": "BENIGN"`) || strings.Contains(afterFirst, "SENSITIVE") {
			t.Fatalf("prompt for call-2 does not show call-2's input:\n%s", afterFirst)
		}
	}
	// After the decisions the headers read approved/denied, and the sections
	// are collapsed again (no input shown).
	final := tt.renderer.portcarrytuiBody()
	if !strings.Contains(final, " approved ╮") || !strings.Contains(final, " denied ╮") || strings.Contains(final, "SENSITIVE") || strings.Contains(final, "BENIGN") {
		t.Fatalf("sections not restored after the decisions:\n%s", final)
	}
}

func (r *TerminalRenderer) portcarrytuiBody() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scrollOffset = r.bodyLineCount()
	r.scrollOffset = r.clampScrollOffset(r.scrollOffset)
	return stripANSI(strings.Join(r.visibleBodyLines(), "\n"))
}

// TS "shows the input ... in every tool display mode": hidden adds a section
// for the decision and removes it afterwards; collapsed expands it.
func TestPortcarrytuiApprovalShownInEveryToolMode(t *testing.T) {
	for _, mode := range []TerminalPartDisplayMode{TerminalPartDisplayCollapsed, TerminalPartDisplayHidden} {
		t.Run(string(mode), func(t *testing.T) {
			tt := portcarrytuiNew(nil, 60, 16, mode)
			tt.render(t, portcarrytuiToolCall("call-1", "record", map[string]interface{}{"operation": "read_public_status"}))
			sectionsBefore := len(tt.renderer.sections)
			var seen string
			tt.renderer.input = &portcarrytuiKeys{keys: []string{"y"}, before: func(i int) {
				if i == 0 {
					seen = tt.screen()
				}
			}}
			resp, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
				ToolCallID: "call-1", ToolName: "record", Input: map[string]interface{}{"operation": "read_public_status"},
			}, TerminalSessionOptions{})
			if err != nil || !resp.Approved {
				t.Fatalf("resp=%+v err=%v", resp, err)
			}
			if !strings.Contains(seen, `"operation": "read_public_status"`) || !strings.Contains(seen, "approve? y/n") {
				t.Fatalf("input not shown in %s mode:\n%s", mode, seen)
			}
			if len(tt.renderer.sections) != sectionsBefore {
				t.Fatalf("sections = %d, want %d (the decision section must not stay)", len(tt.renderer.sections), sectionsBefore)
			}
			if mode == TerminalPartDisplayCollapsed {
				if last := tt.renderer.sections[len(tt.renderer.sections)-1]; !last.collapsed || last.rightTitle != "approved" {
					t.Fatalf("collapsed section not restored: %+v", last)
				}
			}
		})
	}
}

// TS "keeps the tool call awaiting approval in view when the terminal is
// resized".
func TestPortcarrytuiApprovalStaysInViewOnResize(t *testing.T) {
	tt := portcarrytuiNew(nil, 48, 14, "")
	tt.render(t,
		portcarrytuiToolCall("call-1", "shell", map[string]interface{}{"command": "date"}),
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text-1", Text: portcarrytuiLines(10)},
	)
	var resized string
	tt.renderer.input = &portcarrytuiKeys{keys: []string{"n"}, before: func(i int) {
		if i != 0 {
			return
		}
		tt.renderer.mu.Lock()
		tt.renderer.rows = 10
		tt.renderer.handleResizeLocked()
		tt.renderer.mu.Unlock()
		resized = tt.screen()
	}}
	if _, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
		ToolCallID: "call-1", ToolName: "shell", Input: map[string]interface{}{"command": "date"},
	}, TerminalSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resized, "╭ Tool · shell") || !strings.Contains(resized, `"command": "date"`) {
		t.Fatalf("approval section out of view after resize:\n%s", resized)
	}
}

// TS 25972d7857 "approves a tool input that is taller than the viewport only
// after its end was shown".
func TestPortcarrytuiTallInputApprovedOnlyAfterEndShown(t *testing.T) {
	steps := make([]string, 12)
	for i := range steps {
		steps[i] = "step " + formatInt(int64(i+1))
	}
	input := map[string]interface{}{"commands": steps}
	tt := portcarrytuiNew(nil, 48, 14, "")
	var s0, s1, s2, s3 string
	keys := &portcarrytuiKeys{keys: []string{"y", "\x1b[B", "y", "\x1b[6~", "y"}}
	keys.before = func(i int) {
		switch i {
		case 1:
			s0 = tt.screen() // after the ignored y: start of the input
		case 2:
			s1 = tt.screen() // after one line down
		case 3:
			s2 = tt.screen() // after the second y, still ignored
		case 4:
			s3 = tt.screen() // end shown
		}
	}
	tt.renderer.input = keys
	resp, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
		ToolCallID: "call-1", ToolName: "shell", Input: input,
	}, TerminalSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Approved || keys.i != 5 {
		t.Fatalf("resp=%+v keys consumed=%d, want approval by the last y", resp, keys.i)
	}
	if !strings.Contains(s0, `"step 5"`) || strings.Contains(s0, `"step 12"`) || !strings.Contains(s0, "Scroll to the end to approve · n to deny · ↑") {
		t.Fatalf("start of input / status wrong:\n%s", s0)
	}
	if !strings.Contains(s1, `"step 6"`) || !strings.Contains(s1, "Scroll to the end to approve") {
		t.Fatalf("after one line down:\n%s", s1)
	}
	if !strings.Contains(s2, "Scroll to the end to approve") {
		t.Fatalf("y approved before the end was shown:\n%s", s2)
	}
	if !strings.Contains(s3, `"step 12"`) || !strings.Contains(s3, "│ Approve tool shell? y/n") || strings.Contains(s3, "Scroll to the end") {
		t.Fatalf("end of input not shown:\n%s", s3)
	}
}

// TS "denies a tool input that is taller than the viewport without scrolling".
func TestPortcarrytuiTallInputDeniedWithoutScrolling(t *testing.T) {
	steps := make([]string, 12)
	for i := range steps {
		steps[i] = "step " + formatInt(int64(i+1))
	}
	tt := portcarrytuiNew(&portcarrytuiKeys{keys: []string{"n"}}, 48, 14, "")
	resp, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
		ToolCallID: "call-1", ToolName: "shell", Input: map[string]interface{}{"commands": steps},
	}, TerminalSessionOptions{})
	if err != nil || resp.Approved {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
}

// TS "shows invisible format characters in tool cards and approval prompts".
func TestPortcarrytuiFormatCharactersEscapedInToolAndApproval(t *testing.T) {
	tt := portcarrytuiNew(&portcarrytuiKeys{keys: []string{"n"}}, 80, 24, TerminalPartDisplayFull)
	tt.render(t,
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "t", Title: "ti\u2066tle"}},
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "t", Result: "admin\u200b@example.com"}},
	)
	if _, err := tt.renderer.ReadToolApproval(context.Background(), AgentTUIToolApprovalRequest{
		ToolCallID: "call-2", ToolName: "na\u2067me", Input: "report_\u202efdp.exe\U000e0041",
	}, TerminalSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := tt.out.String()
	for _, r := range raw {
		if r == '\u2066' || r == '\u2067' || r == '\u200b' || r == '\u202e' || r == '\U000e0041' {
			t.Fatalf("raw format character %U reached the terminal", r)
		}
	}
	for _, want := range []string{`Tool · ti\u{2066}tle`, `Approve tool na\u{2067}me? y/n`, `admin\u{200b}@example.com`, `report_\u{202e}fdp.exe\u{e0041}`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

// TS "keeps format characters in assistant text".
func TestPortcarrytuiAssistantTextKeepsFormatCharacters(t *testing.T) {
	tt := portcarrytuiNew(nil, 60, 14, "")
	tt.render(t, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text-1", Text: "\U0001f469\u200d\U0001f4bb"})
	if !strings.Contains(tt.out.String(), "\U0001f469\u200d\U0001f4bb") {
		t.Fatal("zero-width joiner was escaped in assistant text")
	}
}

// TS "shows tool input, output, and errors without markdown formatting".
func TestPortcarrytuiToolSectionsWithoutMarkdown(t *testing.T) {
	tt := portcarrytuiNew(nil, 72, 30, TerminalPartDisplayFull)
	tt.render(t,
		portcarrytuiToolCall("call-1", "shell", map[string]interface{}{"command": "rm *.log *.tmp"}),
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "shell", Result: map[string]interface{}{"removed_files": []interface{}{"__status__.log"}}}},
		portcarrytuiToolCall("call-2", "shell", map[string]interface{}{"command": "echo `whoami`"}),
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-2", ToolName: "shell", Error: errTest("invalid_request_error: `whoami` is not allowed")}},
	)
	screen := tt.screen()
	for _, want := range []string{
		`"command": "rm *.log *.tmp"`,
		`"removed_files": [`,
		`"__status__.log"`,
		"Tool Error · shell",
		"echo `whoami`",
		"invalid_request_error: `whoami` is not allowed",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q in:\n%s", want, screen)
		}
	}
}
