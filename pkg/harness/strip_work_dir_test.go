package harness

import (
	"encoding/json"
	"testing"
)

// Ports TS internal/strip-work-dir.test.ts.

const testWorkDir = "/vercel/sandbox/claude-code-abc123"

func TestStripWorkDir_ToolCallInput(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"path": testWorkDir + "/src/foo.ts"})
	part := &ToolCallPart{ToolCallID: "c1", ToolName: "readFile", Input: string(input)}
	out := StripWorkDir(part, testWorkDir).(*ToolCallPart)
	want, _ := json.Marshal(map[string]any{"path": "src/foo.ts"})
	if out.Input != string(want) {
		t.Fatalf("Input = %q, want %q", out.Input, string(want))
	}
}

func TestStripWorkDir_ToolInputDelta(t *testing.T) {
	delta, _ := json.Marshal(map[string]any{"path": testWorkDir + "/src/foo.ts"})
	part := &ToolInputDeltaPart{ID: "c1", Delta: string(delta)}
	out := StripWorkDir(part, testWorkDir).(*ToolInputDeltaPart)
	want, _ := json.Marshal(map[string]any{"path": "src/foo.ts"})
	if out.Delta != string(want) {
		t.Fatalf("Delta = %q, want %q", out.Delta, string(want))
	}
}

func TestStripWorkDir_ToolResultFreeformString(t *testing.T) {
	part := &ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: testWorkDir + "/a.ts\n" + testWorkDir + "/b.ts\n"}
	out := StripWorkDir(part, testWorkDir).(*ToolResultPart)
	if out.Result != "a.ts\nb.ts\n" {
		t.Fatalf("Result = %q, want %q", out.Result, "a.ts\nb.ts\n")
	}
}

func TestStripWorkDir_ToolResultNested(t *testing.T) {
	part := &ToolResultPart{
		ToolCallID: "c1", ToolName: "grep",
		Result: map[string]any{
			"matches": []any{
				map[string]any{"file": testWorkDir + "/src/a.ts", "line": 1},
				map[string]any{"file": testWorkDir + "/src/b.ts", "line": 2},
			},
			"cwd": testWorkDir,
		},
	}
	out := StripWorkDir(part, testWorkDir).(*ToolResultPart)
	result := out.Result.(map[string]any)
	if result["cwd"] != "." {
		t.Fatalf("cwd = %v, want '.'", result["cwd"])
	}
	matches := result["matches"].([]any)
	if matches[0].(map[string]any)["file"] != "src/a.ts" || matches[1].(map[string]any)["file"] != "src/b.ts" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestStripWorkDir_BareReferenceBecomesDot(t *testing.T) {
	part := &FileChangePart{Event: FileChangeModify, Path: testWorkDir}
	out := StripWorkDir(part, testWorkDir).(*FileChangePart)
	if out.Path != "." {
		t.Fatalf("Path = %q, want '.'", out.Path)
	}
}

func TestStripWorkDir_BareWorkDirFollowedByDelimiter(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"whitespace", "cd /work && pwd", "cd . && pwd"},
		{"a quote in JSON", `{"cwd":"/work"}`, `{"cwd":"."}`},
		{"a shell delimiter", "cd /work;pwd", "cd .;pwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			part := &ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: tc.input}
			out := StripWorkDir(part, "/work").(*ToolCallPart)
			if out.Input != tc.want {
				t.Fatalf("Input = %q, want %q", out.Input, tc.want)
			}
		})
	}
}

func TestStripWorkDir_FileChangePath(t *testing.T) {
	part := &FileChangePart{Event: FileChangeCreate, Path: testWorkDir + "/notes.md"}
	out := StripWorkDir(part, testWorkDir).(*FileChangePart)
	if out.Path != "notes.md" {
		t.Fatalf("Path = %q, want notes.md", out.Path)
	}
}

func TestStripWorkDir_PreservesLongerPathSegments(t *testing.T) {
	part := &ToolResultPart{
		ToolCallID: "c1", ToolName: "bash",
		Result: map[string]any{
			"command": "ls .github/workflows && echo origin/workcell && cat /work/a.ts",
			"path":    "/work/.github/workflows/ci.yml",
			"branch":  "origin/workcell/topic",
		},
	}
	out := StripWorkDir(part, "/work").(*ToolResultPart)
	result := out.Result.(map[string]any)
	if result["command"] != "ls .github/workflows && echo origin/workcell && cat a.ts" {
		t.Fatalf("command = %q", result["command"])
	}
	if result["path"] != ".github/workflows/ci.yml" {
		t.Fatalf("path = %q", result["path"])
	}
	if result["branch"] != "origin/workcell/topic" {
		t.Fatalf("branch = %q", result["branch"])
	}
}

func TestStripWorkDir_RepeatedWorkDirTextAfterEmbeddedOccurrence(t *testing.T) {
	value := "origin/work/work/foo"

	toolCall := StripWorkDir(&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: value}, "/work").(*ToolCallPart)
	toolResult := StripWorkDir(&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: value}, "/work").(*ToolResultPart)
	fileChange := StripWorkDir(&FileChangePart{Event: FileChangeModify, Path: value}, "/work").(*FileChangePart)

	strip := NewToolInputWorkDirStripper("/work")
	var streamed string
	for _, p := range []StreamPart{
		&ToolInputStartPart{ID: "c1", ToolName: "bash"},
		&ToolInputDeltaPart{ID: "c1", Delta: value},
		&ToolInputEndPart{ID: "c1"},
	} {
		for _, out := range strip(p) {
			if d, ok := out.(*ToolInputDeltaPart); ok {
				streamed += d.Delta
			}
		}
	}

	if toolCall.Input != streamed {
		t.Fatalf("toolCall.Input = %q, streamed = %q", toolCall.Input, streamed)
	}
	if toolResult.Result != streamed {
		t.Fatalf("toolResult.Result = %v, streamed = %q", toolResult.Result, streamed)
	}
	if fileChange.Path != streamed {
		t.Fatalf("fileChange.Path = %q, streamed = %q", fileChange.Path, streamed)
	}
	if streamed != value {
		t.Fatalf("streamed = %q, want %q", streamed, value)
	}
}

func TestStripWorkDir_StreamedBareWorkDirFollowedByDelimiter(t *testing.T) {
	cases := []struct {
		name   string
		deltas []string
		want   string
	}{
		{"whitespace", []string{"cd /work", " && pwd"}, "cd . && pwd"},
		{"a quote in JSON", []string{`{"cwd":"/work`, `"}`}, `{"cwd":"."}`},
		{"a shell delimiter", []string{"cd /work", ";pwd"}, "cd .;pwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			strip := NewToolInputWorkDirStripper("/work")
			var out string
			parts := []StreamPart{&ToolInputStartPart{ID: "c1", ToolName: "bash"}}
			for _, d := range tc.deltas {
				parts = append(parts, &ToolInputDeltaPart{ID: "c1", Delta: d})
			}
			parts = append(parts, &ToolInputEndPart{ID: "c1"})
			for _, p := range parts {
				for _, o := range strip(p) {
					if d, ok := o.(*ToolInputDeltaPart); ok {
						out += d.Delta
					}
				}
			}
			if out != tc.want {
				t.Fatalf("out = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestStripWorkDir_PreservesEmbeddedWorkDirSplitAcrossDeltas(t *testing.T) {
	strip := NewToolInputWorkDirStripper("/work")
	parts := []StreamPart{
		&ToolInputStartPart{ID: "c1", ToolName: "bash"},
		&ToolInputDeltaPart{ID: "c1", Delta: `{"command":"ls .github/wor`},
		&ToolInputDeltaPart{ID: "c1", Delta: "kflows && cat /wo"},
		&ToolInputDeltaPart{ID: "c1", Delta: `rk/a.ts"}`},
		&ToolInputEndPart{ID: "c1"},
	}
	var out string
	for _, p := range parts {
		for _, o := range strip(p) {
			if d, ok := o.(*ToolInputDeltaPart); ok {
				out += d.Delta
			}
		}
	}
	want := `{"command":"ls .github/workflows && cat a.ts"}`
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestStripWorkDir_PreservesWorkDirPrefixAfterNonBoundaryCharacter(t *testing.T) {
	strip := NewToolInputWorkDirStripper("/work")
	parts := []StreamPart{
		&ToolInputStartPart{ID: "c1", ToolName: "bash"},
		&ToolInputDeltaPart{ID: "c1", Delta: "foo"},
		&ToolInputDeltaPart{ID: "c1", Delta: "/work/bar"},
		&ToolInputEndPart{ID: "c1"},
	}
	var out string
	for _, p := range parts {
		for _, o := range strip(p) {
			if d, ok := o.(*ToolInputDeltaPart); ok {
				out += d.Delta
			}
		}
	}
	if out != "foo/work/bar" {
		t.Fatalf("out = %q, want foo/work/bar", out)
	}
}

func TestStripWorkDir_PassesThroughUnrelatedVariants(t *testing.T) {
	part := &TextDeltaPart{ID: "t1", Delta: "wrote " + testWorkDir + "/foo.ts"}
	out := StripWorkDir(part, testWorkDir)
	if out != StreamPart(part) {
		t.Fatalf("expected the same part instance back")
	}
}
