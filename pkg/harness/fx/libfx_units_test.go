package fx

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports of TS fx-resume-state.test.ts, fx-remote-ops.test.ts and the
// "tool conversion and permissions" cases of fx-harness.test.ts.

var sampleState = &fxStoredState{Version: 1, SDKVersion: "0.0.14", Checkpoint: "AQID"}

func TestStateStoreRoundTripsSessionDirectories(t *testing.T) {
	cases := []struct{ sessionID, segment string }{
		{"session-1", "session-1"},
		{"../unsafe/session-id", "..%2Funsafe%2Fsession-id"},
		{"a/b", "a%2Fb"},
		{"a%2Fb", "a%252Fb"},
		{"..", "%2E%2E"},
		{"", "%"},
	}
	for _, c := range cases {
		sb := newFakeSandbox()
		sb.home = "/sandbox/home"
		ctx := context.Background()
		store, err := newStateStore(ctx, sb, c.sessionID, "/sandbox/work", "")
		if err != nil {
			t.Fatal(err)
		}
		dir := "/sandbox/home/.ai-sdk-harness/.agent-runs/" + c.segment + "/fx"
		file := dir + "/state.json"
		cmds := sb.commandList()
		if want := "mkdir -p -- '" + dir + "' && chmod 700 -- '" + dir + "'"; cmds[1] != want {
			t.Errorf("%q: mkdir = %q", c.sessionID, cmds[1])
		}
		if store.fileName != "state.json" {
			t.Errorf("file name %q", store.fileName)
		}
		content, _ := jsstr.Marshal(sampleState)
		if err := store.save(ctx, content); err != nil {
			t.Fatal(err)
		}
		if len(sb.writes) != 1 || sb.writes[0].Path != file+".tmp" || sb.writes[0].Content != string(content) {
			t.Errorf("writes = %+v", sb.writes)
		}
		if got := sb.commandList()[2]; got != "mv -- '"+file+".tmp' '"+file+"'" {
			t.Errorf("mv = %q", got)
		}
		if len(sb.files) != 1 || sb.files[file] == "" {
			t.Errorf("files = %v", sb.files)
		}
		restored, err := newStateStore(ctx, sb, c.sessionID, "/sandbox/work", "")
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := restored.load(ctx)
		if err != nil || !reflect.DeepEqual(loaded, sampleState) {
			t.Errorf("load = %+v, %v", loaded, err)
		}
		if len(sb.reads) != 1 || sb.reads[0] != file {
			t.Errorf("reads = %v", sb.reads)
		}
	}
}

func TestStateStoreSeparatesSessionIDs(t *testing.T) {
	sb := newFakeSandbox()
	sb.home = "/sandbox/home"
	for _, id := range []string{"a/b", "a%2Fb"} {
		store, err := newStateStore(context.Background(), sb, id, "/sandbox/work", "")
		if err != nil {
			t.Fatal(err)
		}
		content, _ := jsstr.Marshal(sampleState)
		_ = store.save(context.Background(), content)
	}
	for _, f := range []string{"/sandbox/home/.ai-sdk-harness/.agent-runs/a%2Fb/fx/state.json", "/sandbox/home/.ai-sdk-harness/.agent-runs/a%252Fb/fx/state.json"} {
		if _, ok := sb.files[f]; !ok {
			t.Errorf("missing %s", f)
		}
	}
}

func TestStateStoreRejectsStateInsideWorkspace(t *testing.T) {
	for _, work := range []string{"/sandbox/home", "/sandbox/home/.ai-sdk-harness/.agent-runs/session-1", "/sandbox/home/.ai-sdk-harness/.agent-runs/session-1/fx"} {
		sb := newFakeSandbox()
		sb.home = "/sandbox/home"
		_, err := newStateStore(context.Background(), sb, "session-1", work, "")
		if err == nil || err.Error() != "fx private state must be outside sessionWorkDir." {
			t.Errorf("%s: %v", work, err)
		}
		if len(sb.commandList()) != 1 {
			t.Errorf("%s: ran %v", work, sb.commandList())
		}
	}
}

func TestStateStoreUsesLifecycleFileName(t *testing.T) {
	sb := newFakeSandbox()
	sb.home = "/sandbox/home"
	store, err := newStateStore(context.Background(), sb, "session-1", "/sandbox/work", "checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := jsstr.Marshal(sampleState)
	_ = store.save(context.Background(), content)
	if store.fileName != "checkpoint.json" {
		t.Error(store.fileName)
	}
	if _, ok := sb.files["/sandbox/home/.ai-sdk-harness/.agent-runs/session-1/fx/checkpoint.json"]; !ok {
		t.Errorf("files = %v", sb.files)
	}
	if loaded, err := store.load(context.Background()); err != nil || !reflect.DeepEqual(loaded, sampleState) {
		t.Errorf("load = %+v, %v", loaded, err)
	}
}

func TestStateStoreRejectsMissingAndInvalidState(t *testing.T) {
	sb := newFakeSandbox()
	sb.home = "/sandbox/home"
	store, _ := newStateStore(context.Background(), sb, "session-1", "/sandbox/work", "")
	if _, err := store.load(context.Background()); err == nil || err.Error() != "Cannot resume fx: the saved state file is missing." {
		t.Errorf("missing = %v", err)
	}
	for _, content := range []string{
		"not json",
		`{"version":2,"sdkVersion":"0.0.14"}`,
		`{"version":1,"sdkVersion":"0.0.99"}`,
		`{"version":1,"sdkVersion":"0.0.14","provider":"other"}`,
		`{"version":1,"sdkVersion":"0.0.14","turn":{"id":"t"}}`,
		`{"version":1,"sdkVersion":"0.0.14","checkpoint":null}`,
		`{"version":1,"sdkVersion":"0.0.14","__proto__":{}}`,
	} {
		sb.files["/sandbox/home/.ai-sdk-harness/.agent-runs/session-1/fx/state.json"] = content
		_, err := store.load(context.Background())
		var inc *IncompatibleStateError
		if !errors.As(err, &inc) || err.Error() != "Cannot resume fx: the saved state is invalid or incompatible." {
			t.Errorf("%s: %v", content, err)
		}
	}
	// A 0.0.13 state file still loads.
	sb.files["/sandbox/home/.ai-sdk-harness/.agent-runs/session-1/fx/state.json"] = `{"version":1,"sdkVersion":"0.0.13","provider":"gateway"}`
	if _, err := store.load(context.Background()); err != nil {
		t.Errorf("0.0.13 state: %v", err)
	}
}

func TestStateStoreFailedWriteFailsLaterWrites(t *testing.T) {
	sb := newFakeSandbox()
	store, _ := newStateStore(context.Background(), sb, "s", "/workspace", "")
	sb.handle = nil
	failing := &failingMv{fakeSandbox: sb}
	store.sandbox = failing
	if err := store.save(context.Background(), []byte("{}")); err == nil || !strings.Contains(err.Error(), "Unable to persist fx state: denied") {
		t.Fatalf("save = %v", err)
	}
	failing.ok = true
	if err := store.save(context.Background(), []byte("{}")); err == nil {
		t.Error("a write after a failed one succeeded")
	}
}

type failingMv struct {
	*fakeSandbox
	ok bool
}

func (f *failingMv) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if strings.HasPrefix(opts.Command, "mv --") && !f.ok {
		return providerutils.SandboxRunResult{ExitCode: 1, Stderr: "denied"}, nil
	}
	return f.fakeSandbox.Run(ctx, opts)
}

func TestLifecycleDataSchemas(t *testing.T) {
	h, _ := newMockHarness(t, Settings{})
	for _, data := range []string{`{"version":1,"stateFileName":"state.json"}`, `{"version":1,"stateFileName":"a.b-c_d.json","extra":true}`} {
		if err := h.ValidateLifecycleStateData(json.RawMessage(data)); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}
	for _, data := range []string{`{"version":2,"stateFileName":"state.json"}`, `{"version":1,"stateFileName":"../x.json"}`, `{"version":1}`, `[]`} {
		if err := h.ValidateLifecycleStateData(json.RawMessage(data)); err == nil {
			t.Errorf("%s accepted", data)
		}
	}
	legacy := json.RawMessage(`{"agentIdentity":"fx","acpSessionId":"s1","bridge":{"port":1,"token":"t","lastSeenEventId":0}}`)
	var inc *IncompatibleStateError
	if err := h.ValidateLifecycleStateData(legacy); !errors.As(err, &inc) || !strings.Contains(err.Error(), "RuntimeSandboxACP") {
		t.Errorf("legacy under libfx = %v", err)
	}
	if _, err := h.DoStart(context.Background(), harness.StartOptions{SandboxSession: newFakeSandbox(), SessionID: "x", SessionWorkDir: "/workspace",
		ResumeFrom: &harness.ResumeSessionState{Type: "resume-session", HarnessID: "fx", SpecificationVersion: "harness-v1", Data: legacy}}); !errors.As(err, &inc) {
		t.Errorf("DoStart legacy = %v", err)
	}
	if _, err := h.DoStart(context.Background(), harness.StartOptions{SandboxSession: newFakeSandbox(), SessionID: "x", SessionWorkDir: "/workspace",
		ResumeFrom: &harness.ResumeSessionState{Type: "resume-session", HarnessID: "codex", SpecificationVersion: "harness-v1", Data: json.RawMessage(`{}`)}}); err == nil || err.Error() != "Cannot import lifecycle state from a different harness." {
		t.Errorf("other harness = %v", err)
	}
	old, err := CreateFx()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.(harness.LifecycleStateValidator).ValidateLifecycleStateData(json.RawMessage(`{"version":1,"stateFileName":"state.json"}`)); !errors.As(err, &inc) || !strings.Contains(err.Error(), "RuntimeLibfx") {
		t.Errorf("libfx under sandbox-ACP = %v", err)
	}
	if err := old.(harness.LifecycleStateValidator).ValidateLifecycleStateData(legacy); err != nil {
		t.Errorf("legacy under sandbox-ACP = %v", err)
	}
}

// remote ops

func newRemote(canonical string) (*remoteOps, *fakeSandbox) {
	sb := newFakeSandbox()
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "realpath") {
			return providerutils.SandboxRunResult{Stdout: b64(canonical + "\x00")}, nil
		}
		return providerutils.SandboxRunResult{Stdout: "sandbox output"}, nil
	}
	sb.files["/workspace/file"] = "sandbox file"
	return newRemoteOps(sb, "/workspace"), sb
}

func TestRemoteOpsRejectEscapesBeforeSandbox(t *testing.T) {
	r, sb := newRemote("/workspace")
	ctx := context.Background()
	if _, err := r.execute(ctx, "read", json.RawMessage(`{"file_path":"/etc/passwd"}`)); err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("read = %v", err)
	}
	if _, err := r.execute(ctx, "write", json.RawMessage(`{"file_path":"../outside","content":"no"}`)); err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("write = %v", err)
	}
	if len(sb.commandList()) != 0 || len(sb.reads) != 0 || len(sb.writes) != 0 {
		t.Error("touched the sandbox")
	}
}

func TestRemoteOpsRejectSymlinkEscapes(t *testing.T) {
	sb := newFakeSandbox()
	calls := 0
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		calls++
		if calls == 1 {
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		}
		return providerutils.SandboxRunResult{Stdout: b64("/outside/secret\x00")}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	if _, err := r.execute(context.Background(), "read", json.RawMessage(`{"file_path":"link/secret"}`)); err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("read = %v", err)
	}
	if len(sb.reads) != 0 {
		t.Error("read the file")
	}
}

func TestRemoteOpsRunShellInWorkspaceWithCancellation(t *testing.T) {
	sb := newFakeSandbox()
	var gotCtx context.Context
	var got providerutils.SandboxProcessOptions
	sb.handle = func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		gotCtx, got = ctx, opts
		return providerutils.SandboxRunResult{Stdout: "out", ExitCode: 3}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	ctx, cancel := context.WithCancel(context.Background())
	out, err := r.execute(ctx, "bash", json.RawMessage(`{"command":"printf sandbox"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "printf sandbox" || got.WorkingDirectory != "/workspace" {
		t.Errorf("run = %+v", got)
	}
	cancel()
	if gotCtx.Err() == nil {
		t.Error("cancellation was not forwarded")
	}
	if data, _ := jsstr.Marshal(out); string(data) != `{"exitCode":3,"stdout":"out","stderr":""}` {
		t.Errorf("output = %s", data)
	}
}

func TestRemoteOpsDoNotMaskCanonicalizationErrors(t *testing.T) {
	sb := newFakeSandbox()
	sb.handle = func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		return providerutils.SandboxRunResult{ExitCode: 1, Stderr: "failed"}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	if _, err := r.execute(context.Background(), "read", json.RawMessage(`{"file_path":"file"}`)); err == nil || !strings.Contains(err.Error(), "Unable to resolve sandbox path") {
		t.Errorf("read = %v", err)
	}
}

func TestRemoteOpsEditGrepGlobLs(t *testing.T) {
	sb := newFakeSandbox()
	sb.files["/workspace/a.txt"] = "one two two"
	var last string
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		last = opts.Command
		switch {
		case strings.Contains(opts.Command, "realpath"):
			if strings.Contains(opts.Command, "a.txt") {
				return providerutils.SandboxRunResult{Stdout: b64("/workspace/a.txt\x00")}, nil
			}
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		case strings.Contains(opts.Command, "-type f"):
			return providerutils.SandboxRunResult{Stdout: "/workspace/src/b.go\x00/workspace/a.txt\x00/workspace/src/deep/c.go\x00"}, nil
		case strings.Contains(opts.Command, "-maxdepth 1"):
			return providerutils.SandboxRunResult{Stdout: "src\x00a.txt\x00"}, nil
		}
		return providerutils.SandboxRunResult{}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	ctx := context.Background()
	if _, err := r.execute(ctx, "edit", json.RawMessage(`{"file_path":"a.txt","old_string":"two","new_string":"2"}`)); err == nil || !strings.Contains(err.Error(), "occurs more than once") {
		t.Errorf("ambiguous edit = %v", err)
	}
	if out, err := r.execute(ctx, "edit", json.RawMessage(`{"file_path":"a.txt","old_string":"one","new_string":"1"}`)); err != nil || out != "Edited a.txt" || sb.files["/workspace/a.txt"] != "1 two two" {
		t.Errorf("edit = %v, %v, %q", out, err, sb.files["/workspace/a.txt"])
	}
	if out, _ := r.execute(ctx, "grep", json.RawMessage(`{"pattern":"x","ignoreCase":true,"glob":"*.go","context":2,"limit":5.7}`)); out != "No matches found" ||
		last != `grep '-r' '-n' '--binary-files=without-match' '-i' '--include=*.go' '-C' '2' -- 'x' '/workspace' | head -n 5` {
		t.Errorf("grep = %v: %s", out, last)
	}
	if out, err := r.execute(ctx, "glob", json.RawMessage(`{"pattern":"**/*.go"}`)); err != nil || !reflect.DeepEqual(out, []string{"src/b.go", "src/deep/c.go"}) {
		t.Errorf("glob = %v, %v", out, err)
	}
	if out, _ := r.execute(ctx, "glob", json.RawMessage(`{"pattern":"*.txt","limit":1}`)); !reflect.DeepEqual(out, []string{"a.txt"}) {
		t.Errorf("glob = %v", out)
	}
	if out, _ := r.execute(ctx, "ls", json.RawMessage(`{}`)); !reflect.DeepEqual(out, []string{"a.txt", "src"}) || !strings.HasSuffix(last, `-mindepth 1 -maxdepth 1 -printf '%f\0'`) {
		t.Errorf("ls = %v: %s", out, last)
	}
}

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "a.go", true}, {"*.go", "src/a.go", false}, {"**/*.go", "a.go", true}, {"**/*.go", "x/y/a.go", true},
		{"src/**", "src/a/b", true}, {"?.txt", "a.txt", true}, {"[ab].txt", "c.txt", false}, {"[!ab].txt", "c.txt", true},
		{"*.{go,ts}", "a.ts", true}, {"*.{go,ts}", "a.js", false}, {"a+b.txt", "a+b.txt", true},
		// Node path.matchesGlob (minimatch, dot: false), checked with node v24.
		{"**/*", ".git/config", false}, {"**/*.go", "a/.b/c.go", false}, {"*.js", ".eslintrc.js", false},
		{".*.js", ".eslintrc.js", true}, {"*.js", ".js", false}, {"**/*.go", "b.go", true},
		{"a/*", "a/.x", false}, {"a/.*", "a/.x", true}, {"**/b", ".a/b", false}, {".a/*", ".a/b", true},
		{"x/**", "x/.a/b", false}, {"x/**", "x/a", true}, {"a/**/b", "a/b", true}, {"a/**", "a", false},
		{"{src,lib}/**/*.go", "lib/x/y.go", true}, {"{src,lib}/*.go", "cmd/y.go", false}, {"a{b,c{d,e}}", "ace", true},
	}
	for _, c := range cases {
		re, err := globRegexp(c.pattern)
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("%s ~ %s = %v", c.pattern, c.path, got)
		}
	}
}

// tools

func modelSubmission(output types.ToolResultOutput, isError bool) harness.ToolResultSubmission {
	return harness.ToolResultSubmission{ToolCallID: "a", Output: "visible", IsError: isError,
		ToolResult: &harness.ModelToolResult{Type: "tool-result", ToolCallID: "a", ToolName: "tool", Output: output}}
}

func resultJSON(t *testing.T, r *fxToolResult) string {
	t.Helper()
	b, _ := jsstr.Marshal(r)
	return string(b)
}

func TestToToolResultTypedSemantics(t *testing.T) {
	outputs := []types.ToolResultOutput{
		{Type: types.ToolResultOutputText, Value: "text"},
		{Type: types.ToolResultOutputJSON, Value: map[string]any{"count": 2}},
		{Type: types.ToolResultOutputErrorText, Value: "error text"},
		{Type: types.ToolResultOutputErrorJSON, Value: map[string]any{"error": true}},
		{Type: types.ToolResultOutputExecutionDenied},
		{Type: types.ToolResultOutputExecutionDenied, Reason: "Not permitted"},
		{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "first"}, types.TextContentBlock{Text: ""}, types.TextContentBlock{Text: "second"}}},
	}
	want := []string{
		`{"type":"libfx.tool-result","text":"text","images":[],"isError":false}`,
		`{"type":"libfx.tool-result","text":"{\"count\":2}","images":[],"isError":false}`,
		`{"type":"libfx.tool-result","text":"error text","images":[],"isError":true}`,
		`{"type":"libfx.tool-result","text":"{\"error\":true}","images":[],"isError":true}`,
		`{"type":"libfx.tool-result","text":"Tool execution was denied.","images":[],"isError":true}`,
		`{"type":"libfx.tool-result","text":"Not permitted","images":[],"isError":true}`,
		`{"type":"libfx.tool-result","text":"first\nsecond","images":[]}`,
	}
	for i, o := range outputs {
		r, err := toToolResult(modelSubmission(o, false))
		if err != nil {
			t.Fatal(err)
		}
		if got := resultJSON(t, r); got != want[i] {
			t.Errorf("%d: %s", i, got)
		}
		r, _ = toToolResult(modelSubmission(o, true))
		if r.IsError == nil || !*r.IsError {
			t.Errorf("%d: isError submission not kept", i)
		}
	}
}

func TestToToolResultUntypedFallbacks(t *testing.T) {
	for i, c := range []struct {
		output any
		want   string
	}{
		{"raw", `{"type":"libfx.tool-result","text":"raw","images":[]}`},
		{map[string]any{"answer": 42}, `{"type":"libfx.tool-result","text":"{\"answer\":42}","images":[]}`},
		{nil, `{"type":"libfx.tool-result","text":"null","images":[]}`},
	} {
		r, err := toToolResult(harness.ToolResultSubmission{ToolCallID: "a", Output: c.output})
		if err != nil || resultJSON(t, r) != c.want {
			t.Errorf("%d: %s, %v", i, resultJSON(t, r), err)
		}
	}
}

func TestToToolResultImagesAndUnsupportedContent(t *testing.T) {
	r, err := toToolResult(modelSubmission(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.TextContentBlock{Text: "caption"},
		types.FileContentBlock{MediaType: "image/png", Data: []byte{1, 2, 3}},
	}}, false))
	if err != nil {
		t.Fatal(err)
	}
	if got := resultJSON(t, r); got != `{"type":"libfx.tool-result","text":"caption","images":[{"type":"image","mimeType":"image/png","data":"AQID"}]}` {
		t.Errorf("images = %s", got)
	}
	for _, block := range []types.ToolResultContentBlock{
		types.FileContentBlock{MediaType: "application/pdf", Data: []byte{1, 2, 3}},
		types.FileContentBlock{MediaType: "image/png", URL: "https://example.com/image.png"},
	} {
		_, err := toToolResult(modelSubmission(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{block}}, false))
		if err == nil || !strings.Contains(err.Error(), "Harnesses support only text and inline images in tool model output.") {
			t.Errorf("%+v: %v", block, err)
		}
	}
}

func TestToolRequiresApproval(t *testing.T) {
	for _, c := range []struct {
		mode harness.PermissionMode
		kind harness.BuiltinToolUseKind
		want bool
	}{
		{"allow-all", "bash", false}, {"allow-edits", "bash", true}, {"allow-edits", "edit", false},
		{"allow-reads", "edit", true}, {"allow-reads", "readonly", false},
	} {
		if got := toolRequiresApproval(c.mode, c.kind); got != c.want {
			t.Errorf("%s/%s = %v", c.mode, c.kind, got)
		}
	}
}

func TestSkillsInstructions(t *testing.T) {
	got, err := skillsInstructions([]harness.Skill{
		{Name: `a"<&`, Description: "desc", Content: "body", Files: []harness.SkillFile{{Path: "r.md", Content: "res"}}},
		{Name: "b", Content: "only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "<skill name=\"a&quot;&lt;&amp;\">\n<description>desc</description>\nbody\n<resource uri=\"r.md\">\nres\n</resource>\n</skill>\n\n<skill name=\"b\">\nonly\n</skill>"
	if got != want {
		t.Errorf("instructions = %q", got)
	}
	if _, err := skillsInstructions([]harness.Skill{{Name: "x"}, {Name: "x"}}); err == nil {
		t.Error("duplicate skill accepted")
	}
}

// The default (sandbox-ACP) runtime refuses libfx lifecycle state at
// DoStart, for both a resume and a continuation, with the typed error.
func TestLegacyRuntimeDoStartRejectsLibfxState(t *testing.T) {
	old, err := CreateFx()
	if err != nil {
		t.Fatal(err)
	}
	data := json.RawMessage(`{"version":1,"stateFileName":"state.json"}`)
	for name, opts := range map[string]harness.StartOptions{
		"resume":   {SessionID: "x", ResumeFrom: &harness.ResumeSessionState{Type: "resume-session", HarnessID: "fx", SpecificationVersion: "harness-v1", Data: data}},
		"continue": {SessionID: "x", ContinueFrom: &harness.ContinueTurnState{Type: "continue-turn", HarnessID: "fx", SpecificationVersion: "harness-v1", Data: data}},
	} {
		var inc *IncompatibleStateError
		if _, err := old.DoStart(context.Background(), opts); !errors.As(err, &inc) || !strings.Contains(err.Error(), "RuntimeLibfx") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestGlobHostCase checks the glob tool against Node's path.matchesGlob on
// each host: minimatch with nocase off (Linux), nocase on (macOS), and
// nocase on with the win32 platform (Windows), all with nocaseMagicOnly and
// windowsPathsNoEscape. Expected values come from node v24's bundled
// minimatch (`node --expose-internals`, internal/deps/minimatch).
func TestGlobHostCase(t *testing.T) {
	cases := []struct {
		pattern, path          string
		linux, darwin, windows bool
	}{
		{"src/*.ts", "src/A.TS", false, true, true},
		{"src/*.ts", "SRC/a.ts", false, false, false},
		{"**/*.ts", "SRC/a.ts", true, true, true},
		{"readme.md", "README.md", false, false, false},
		{"readme.*", "README.md", false, true, true},
		{"README.md", "README.md", true, true, true},
		{"a/b/*", "a/B/c.txt", false, false, false},
		{"a/?/*", "a/B/c.txt", true, true, true},
		{"a/[b]/*", "a/B/c.txt", false, false, false},
		{"a/[a-c]/*", "a/B/c.txt", false, true, true},
		{"{a,x}/b", "A/b", false, false, false},
		{"*.{js,ts}", "Foo.JS", false, true, true},
		{"a[b]", "AB", false, false, false},
		{"[a]?", "aB", true, true, true},
		{"[!B]", "b", true, false, false},
		{"[^a]", "b", true, true, true},
		{"[bc]", "B", false, true, true},
		{"[K]*", "k", false, false, false},
		{"k*", "K", false, false, false},
		{"s*", "ſ", false, false, false},
		{"[ſ]?", "S", false, false, false},
		{"[r-t]*", "S", false, true, true},
		{"[É]*", "é", false, true, true},
		{"[à-ê]*", "É", false, true, true},
		{"ẞ*", "ß", false, false, false},
		{"ǅ*", "ǆ", false, true, true},
		{"FF*", "ﬀ", false, false, false},
		{"[[:upper:]]", "s", false, true, true},
		{"[[:alpha:]]", "é", true, true, true},
		{"[[:alpha:]]", "1", false, false, false},
		{"[[:graph:]]", "x", true, true, true},
		{"[[:graph:]]", " ", false, false, false},
		{"[![:graph:]]", "a", false, false, false},
		{"[[:digit:]a]", "a", true, true, true},
		{"[a-]", "-", true, true, true},
		{"[]]", "]", true, true, true},
		{"[]a]]", "a]", true, true, true},
		{"[]a]", "b", false, false, false},
		{"[x", "[x", true, true, true},
		{"[*", "[", true, true, true},
		{"a\\b", "a/b", true, true, true},
		{"a/b", "a\\b", false, false, true},
		{"a/*", "a\\b", false, false, true},
		{"*", "a\\b", true, true, false},
		{"x\\*.GO", "x/Y.go", false, true, true},
		{"?", "😀", false, false, false},
		{"A*", "a😀", false, true, true},
		{"**/*", "**/*", true, true, true},
		{"**/*", ".git/config", false, false, false},
		{"dir/*", "Dir/.Hidden", false, false, false},
		{"*/*.go", "DIR/x.Go", false, true, true},
		{"{src,lib}/**/*.GO", "lib/X/Y.go", false, true, true},
		{"[[:alpha:]]?", "a😀", true, true, true},
		{"[[:alpha:]]k", "kK", false, true, true},
		{"[[:alpha:]]ſ", "kS", false, true, true},
		{"[😀]", "😀", false, false, false},
		{"??x", "😀x", true, true, true},
		{"[[:alpha:]]", "ß", true, true, true},
		{"[[:alpha:]]ẞ", "xß", false, true, true},
		{"[[:ascii:]]", "a", true, true, true},
		{"[[:xdigit:]]", "A", true, true, true},
		{"[[:xdigit:]]", "g", false, false, false},
	}
	modes := []struct {
		name          string
		noCase, win32 bool
	}{{"linux", false, false}, {"darwin", true, false}, {"windows", true, true}}
	for _, m := range modes {
		for _, c := range cases {
			g, err := compileGlob(c.pattern, m.noCase, m.win32)
			if err != nil {
				t.Fatalf("%s: %s: %v", m.name, c.pattern, err)
			}
			want := map[string]bool{"linux": c.linux, "darwin": c.darwin, "windows": c.windows}[m.name]
			if got := g.MatchString(c.path); got != want {
				t.Errorf("%s: %q ~ %q = %v, want %v", m.name, c.pattern, c.path, got, want)
			}
		}
	}
}

func TestGlobHostDefaults(t *testing.T) {
	if globNoCase != (runtime.GOOS == "darwin" || runtime.GOOS == "windows") || globWin32Paths != (runtime.GOOS == "windows") {
		t.Fatalf("glob host flags = %v, %v on %s", globNoCase, globWin32Paths, runtime.GOOS)
	}
}
