package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Ports packages/harness/src/agent/observability/file-reporter.test.ts and
// covers the Go-specific file handling (permissions, path validation, Close).

func newTestReporter(t *testing.T, failOnly bool) (*FileReporter, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := NewFileReporter(FileReporterOptions{Dir: dir, FailOnly: failOnly})
	if err != nil {
		t.Fatalf("NewFileReporter: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, dir
}

func readEventLines(t *testing.T, dir string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad jsonl line %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

func kinds(lines []map[string]any) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i], _ = l["kind"].(string)
	}
	return out
}

func fileExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "events.jsonl"))
	return err == nil
}

func testDiag(level DebugLevel, msg string) Diagnostic {
	return Diagnostic{Level: level, Message: msg, Subsystem: "sandbox.log.test", Kind: "log", Source: "test", Stream: "stdout", SessionID: "s1", Timestamp: 1}
}

func reporterStart(callID string) telemetry.TelemetryStartEvent {
	return telemetry.TelemetryStartEvent{
		CallID: callID, OperationType: "ai.harness", ModelID: "m",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "what is 2+2?"}}}},
		System:   "be terse",
	}
}

func reporterSettings() *telemetry.Options { return telemetry.DefaultSettings() }

func TestFileReporter_RecordsErrorAndFlushesFailedTurnWithoutOnEnd(t *testing.T) {
	r, dir := newTestReporter(t, true)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	r.OnError(ctx, telemetry.TelemetryErrorEvent{CallID: "call-1", Error: errors.New("bridge failed")})

	lines := readEventLines(t, dir)
	if got := kinds(lines); !reflect.DeepEqual(got, []string{"turn-start", "error"}) {
		t.Fatalf("kinds = %v", got)
	}
	errObj := lines[1]["error"].(map[string]any)
	if errObj["message"] != "bridge failed" || errObj["name"] == "" {
		t.Fatalf("error = %v", errObj)
	}
}

func TestFileReporter_NilErrorPreserved(t *testing.T) {
	r, dir := newTestReporter(t, false)
	r.OnStart(context.Background(), reporterStart("call-1"))
	r.OnError(context.Background(), telemetry.TelemetryErrorEvent{CallID: "call-1"})
	for _, l := range readEventLines(t, dir) {
		if l["kind"] == "error" {
			if v, ok := l["error"]; !ok || v != nil {
				t.Fatalf("error = %v (present=%v), want null", v, ok)
			}
			return
		}
	}
	t.Fatal("no error line")
}

func TestFileReporter_SettlesErrorsByCallIDWhenTurnsOverlap(t *testing.T) {
	r, dir := newTestReporter(t, true)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "call-2"})
	r.OnError(ctx, telemetry.TelemetryErrorEvent{CallID: "call-1", Error: errors.New("first failed")})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-2"})

	var ids []any
	for _, l := range readEventLines(t, dir) {
		ids = append(ids, l["callId"])
	}
	if !reflect.DeepEqual(ids, []any{"call-1", "call-1"}) {
		t.Fatalf("callIds = %v", ids)
	}
}

func TestFileReporter_AbortFlushesTurnWithoutOnEnd(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason error
		want   any
	}{
		{"nil reason", nil, nil},
		{"error reason", errors.New("user stopped"), "user stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newTestReporter(t, false)
			r.OnStart(context.Background(), reporterStart("call-1"))
			r.OnAbort(context.Background(), telemetry.TelemetryAbortEvent{CallID: "call-1", Reason: tc.reason})
			lines := readEventLines(t, dir)
			if got := kinds(lines); !reflect.DeepEqual(got, []string{"turn-start", "turn-abort"}) {
				t.Fatalf("kinds = %v", got)
			}
			reason, has := lines[1]["reason"]
			if tc.want == nil {
				if has {
					t.Fatalf("reason present: %v", reason)
				}
				return
			}
			if reason.(map[string]any)["message"] != tc.want {
				t.Fatalf("reason = %v", reason)
			}
		})
	}
}

func TestFileReporter_FailOnlySkipsCleanAbortAndFlushesErroredAbort(t *testing.T) {
	r, dir := newTestReporter(t, true)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	r.OnAbort(ctx, telemetry.TelemetryAbortEvent{CallID: "call-1"})
	if fileExists(dir) {
		t.Fatal("clean aborted turn must not be written with FailOnly")
	}
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "call-2"})
	r.IngestDiagnostic(testDiag(DebugLevelError, "boom"))
	r.OnAbort(ctx, telemetry.TelemetryAbortEvent{CallID: "call-2"})
	if got := kinds(readEventLines(t, dir)); !reflect.DeepEqual(got, []string{"turn-start", "diagnostic", "turn-abort"}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestFileReporter_WritesUnifiedEventsWithSpansAndDiagnostics(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	in, out := int64(3), int64(1)
	st := reporterSettings()

	r.OnStart(ctx, func() telemetry.TelemetryStartEvent { e := reporterStart("call-1"); e.Settings = st; return e }())
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "call-1", StepNumber: 0})
	r.IngestDiagnostic(testDiag(DebugLevelInfo, "hello from sandbox"))
	r.OnToolExecutionStart(ctx, telemetry.TelemetryToolCallStartEvent{CallID: "call-1", ToolName: "bash", ToolCallID: "c1", Args: map[string]any{"cmd": "ls"}})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "call-1", ToolCallID: "c1", Result: map[string]any{"ok": true}})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "call-1", Settings: st, Text: "4", Usage: telemetry.TelemetryUsage{InputTokens: &in, OutputTokens: &out}})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1", FinishReason: "stop", Usage: telemetry.TelemetryUsage{InputTokens: &in}})

	lines := readEventLines(t, dir)
	want := []string{"turn-start", "step-start", "diagnostic", "tool-start", "tool-end", "step-finish", "turn-finish"}
	if got := kinds(lines); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if d := lines[2]["diagnostic"].(map[string]any); d["message"] != "hello from sandbox" || d["sessionId"] != "s1" || lines[2]["ts"] != float64(1) {
		t.Fatalf("diagnostic = %v ts=%v", d, lines[2]["ts"])
	}
	turnStart := lines[0]
	if turnStart["operationId"] != "ai.harness" || turnStart["modelId"] != "m" {
		t.Fatalf("turn-start = %v", turnStart)
	}
	input := turnStart["input"].(map[string]any)
	if input["instructions"] != "be terse" || len(input["messages"].([]any)) != 1 {
		t.Fatalf("input = %v", input)
	}
	if lines[1]["step"] != float64(0) {
		t.Fatalf("step = %v", lines[1]["step"])
	}
	if lines[3]["toolName"] != "bash" || lines[3]["toolCallId"] != "c1" {
		t.Fatalf("tool-start = %v", lines[3])
	}
	if lines[4]["isError"] != false || lines[4]["output"].(map[string]any)["ok"] != true {
		t.Fatalf("tool-end = %v", lines[4])
	}
	output := lines[5]["output"].([]any)
	if len(output) != 1 || output[0].(map[string]any)["text"] != "4" || output[0].(map[string]any)["type"] != "text" {
		t.Fatalf("step-finish output = %v", output)
	}
	if u := lines[5]["usage"].(map[string]any); u["inputTokens"] != float64(3) || u["outputTokens"] != float64(1) {
		t.Fatalf("usage = %v", u)
	}
	if lines[6]["finishReason"] != "stop" || lines[6]["usage"].(map[string]any)["inputTokens"] != float64(3) {
		t.Fatalf("turn-finish = %v", lines[6])
	}
	// Key order matches the TS object literals.
	raw, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	first := strings.SplitN(string(raw), "\n", 2)[0]
	if !strings.HasPrefix(first, `{"ts":`) || !strings.Contains(first, `"kind":"turn-start","callId":"call-1","operationId":"ai.harness","provider":"","modelId":"m","input":`) {
		t.Fatalf("unexpected key order: %s", first)
	}
}

func TestFileReporter_RecordInputsOutputsFalseSuppressContent(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	st := &telemetry.Options{RecordInputs: telemetry.Bool(false), RecordOutputs: telemetry.Bool(false)}
	e := reporterStart("call-1")
	e.Settings = st
	r.OnStart(ctx, e)
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "call-1"})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "call-1", Settings: st, Text: "4"})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1"})
	for _, l := range readEventLines(t, dir) {
		switch l["kind"] {
		case "turn-start":
			if _, ok := l["input"]; ok {
				t.Fatal("input recorded despite RecordInputs=false")
			}
		case "step-finish":
			if _, ok := l["output"]; ok {
				t.Fatal("output recorded despite RecordOutputs=false")
			}
		}
	}
}

func TestFileReporter_FailOnlySkipsCleanTurnAndWritesErroredTurn(t *testing.T) {
	r, dir := newTestReporter(t, true)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "call-1"})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "call-1"})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1"})
	if fileExists(dir) {
		t.Fatal("clean turn written with FailOnly")
	}
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "call-2", OperationType: "ai.harness", ModelID: "m"})
	r.IngestDiagnostic(testDiag(DebugLevelError, "boom"))
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-2", FinishReason: "error"})
	lines := readEventLines(t, dir)
	hasDiag := false
	for _, l := range lines {
		if l["kind"] == "diagnostic" {
			hasDiag = true
		} else if l["callId"] != "call-2" {
			t.Fatalf("line from another turn: %v", l)
		}
	}
	if !hasDiag {
		t.Fatal("diagnostic missing")
	}
}

func TestFileReporter_FailedToolMarksTurnErrored(t *testing.T) {
	r, dir := newTestReporter(t, true)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	r.OnToolExecutionStart(ctx, telemetry.TelemetryToolCallStartEvent{CallID: "call-1", ToolName: "bash", ToolCallID: "c1"})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "call-1", ToolCallID: "c1", Error: errors.New("exit 1")})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1"})
	lines := readEventLines(t, dir)
	if lines[2]["isError"] != true || len(lines[2]["output"].(map[string]any)) != 0 {
		t.Fatalf("tool-end = %v", lines[2])
	}
}

func TestFileReporter_DiagnosticWithoutOpenTurnWrittenStandalone(t *testing.T) {
	r, dir := newTestReporter(t, false)
	r.IngestDiagnostic(testDiag(DebugLevelInfo, "early"))
	if got := kinds(readEventLines(t, dir)); !reflect.DeepEqual(got, []string{"diagnostic"}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestFileReporter_FileModesAndCustomFileName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "logs")
	r, err := NewFileReporter(FileReporterOptions{Dir: dir, FileName: "run.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	r.OnStart(context.Background(), reporterStart("c"))
	r.OnEnd(context.Background(), telemetry.TelemetryFinishEvent{CallID: "c"})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestFileReporter_RejectsBadOptions(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewFileReporter(FileReporterOptions{}); err == nil {
		t.Fatal("empty Dir accepted")
	}
	// TS does not validate fileName (`${dir}/${fileName}`).
	for _, name := range []string{"../escape.jsonl", "a/b.jsonl", "/abs.jsonl", "..", `a\b.jsonl`} {
		r, err := NewFileReporter(FileReporterOptions{Dir: dir, FileName: name})
		if err != nil {
			t.Fatalf("FileName %q rejected: %v", name, err)
		}
		if want := dir + "/" + name; r.Path() != want {
			t.Fatalf("Path() = %q, want %q", r.Path(), want)
		}
	}
}

func TestFileReporter_CloseFlushesOpenTurnAndDropsLaterRecords(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	r.OnStart(ctx, reporterStart("call-1"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "call-1"})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1"})
	if got := kinds(readEventLines(t, dir)); !reflect.DeepEqual(got, []string{"turn-start"}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestFileReporter_FailOnlyCloseDropsOpenTurn(t *testing.T) {
	r, dir := newTestReporter(t, true)
	r.OnStart(context.Background(), reporterStart("call-1"))
	_ = r.Close()
	if fileExists(dir) {
		t.Fatal("FailOnly must not write an unfinished turn on Close")
	}
}

func TestFileReporter_WriteFailureIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewFileReporter(FileReporterOptions{Dir: filepath.Join(blocker, "sub")})
	if err != nil {
		t.Fatal(err)
	}
	r.OnStart(context.Background(), reporterStart("c"))
	r.OnEnd(context.Background(), telemetry.TelemetryFinishEvent{CallID: "c"}) // must not panic
	if r.Err() == nil {
		t.Fatal("Err() = nil after failed write")
	}
}

func TestFileReporter_ConcurrentTurns(t *testing.T) {
	r, dir := newTestReporter(t, false)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("call-%d", i)
			ctx := context.Background()
			r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: id})
			r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: id})
			r.IngestDiagnostic(testDiag(DebugLevelInfo, "x"))
			r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: id})
		}(i)
	}
	wg.Wait()
	if n := len(readEventLines(t, dir)); n != 80 {
		t.Fatalf("lines = %d, want 80", n)
	}
}

// TestAgent_FileReporterEndToEnd runs a real agent turn with the reporter
// registered, with diagnostics enabled in code, and checks the spans and the
// forwarded diagnostic land in one events.jsonl.
func TestAgent_FileReporterEndToEnd(t *testing.T) {
	dir := t.TempDir()
	reporter, err := NewFileReporter(FileReporterOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reporter.Close() })

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "Hello."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	enabled := true
	tel := telemetry.DefaultSettings()
	tel.Integrations = []telemetry.TelemetryIntegration{reporter}
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, Telemetry: tel,
		Debug: &DebugConfig{Enabled: &enabled, Level: DebugLevelInfo, Subsystems: []string{"bridge"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}

	adapter := mock.harness.(*mockHarnessAdapter)
	adapter.mu.Lock()
	obs := adapter.startCalls[0].Observability
	adapter.mu.Unlock()
	if obs == nil || obs.Report == nil {
		t.Fatal("DoStart got no Observability with Debug enabled")
	}
	if obs.Debug == nil || obs.Debug.Enabled == nil || !*obs.Debug.Enabled || obs.Debug.Level != DebugLevelInfo || !reflect.DeepEqual(obs.Debug.Subsystems, []string{"bridge"}) {
		t.Fatalf("Debug = %+v", obs.Debug)
	}

	genDone := make(chan error, 1)
	go func() {
		_, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session})
		genDone <- err
	}()
	if err := <-genDone; err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readEventLines(t, dir)
	got := kinds(lines)
	if len(got) < 4 || got[0] != "turn-start" || got[len(got)-1] != "turn-finish" {
		t.Fatalf("kinds = %v", got)
	}
	has := map[string]bool{}
	for _, k := range got {
		has[k] = true
	}
	for _, k := range []string{"step-start", "step-finish"} {
		if !has[k] {
			t.Fatalf("missing %s in %v", k, got)
		}
	}
	if lines[0]["operationId"] != "ai.harness" {
		t.Fatalf("turn-start = %v", lines[0])
	}
}

// TestAgent_ObservabilityFansOutDiagnostics checks the Report sink handed to
// DoStart reaches OnLog and DiagnosticConsumer integrations, and that
// diagnostics are off by default.
func TestAgent_ObservabilityFansOutDiagnostics(t *testing.T) {
	t.Setenv("HARNESS_DEBUG", "")
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})

	a, err := NewAgent(AgentSettings{Harness: mock.harness})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()}); err != nil {
		t.Fatal(err)
	}
	adapter := mock.harness.(*mockHarnessAdapter)
	if adapter.startCalls[0].Observability != nil {
		t.Fatal("Observability must be nil when diagnostics are disabled")
	}

	reporter, _ := NewFileReporter(FileReporterOptions{Dir: t.TempDir()})
	t.Cleanup(func() { _ = reporter.Close() })
	tel := telemetry.DefaultSettings()
	tel.Integrations = []telemetry.TelemetryIntegration{reporter}
	var got []Diagnostic
	t.Setenv("HARNESS_DEBUG", "1")
	t.Setenv("HARNESS_DEBUG_LEVEL", "trace")
	t.Setenv("HARNESS_DEBUG_SUBSYSTEMS", " bridge , sandbox ,")
	a2, err := NewAgent(AgentSettings{Harness: mock.harness, Telemetry: tel, OnLog: func(d Diagnostic) { got = append(got, d) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a2.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()}); err != nil {
		t.Fatal(err)
	}
	obs := adapter.startCalls[1].Observability
	if obs == nil {
		t.Fatal("HARNESS_DEBUG=1 did not enable diagnostics")
	}
	if obs.Debug.Level != DebugLevelTrace || !reflect.DeepEqual(obs.Debug.Subsystems, []string{"bridge", "sandbox"}) {
		t.Fatalf("env-resolved Debug = %+v", obs.Debug)
	}
	reporter.OnStart(context.Background(), telemetry.TelemetryStartEvent{CallID: "c"})
	obs.Report(testDiag(DebugLevelWarn, "line"))
	if len(got) != 1 || got[0].Message != "line" {
		t.Fatalf("OnLog got %v", got)
	}
	reporter.mu.Lock()
	n := len(reporter.turns["c"].lines)
	reporter.mu.Unlock()
	if n != 2 { // turn-start + diagnostic
		t.Fatalf("reporter buffered %d lines, want 2", n)
	}

	// Code is authoritative over env.
	off := false
	a3, _ := NewAgent(AgentSettings{Harness: mock.harness, Debug: &DebugConfig{Enabled: &off}})
	if _, err := a3.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()}); err != nil {
		t.Fatal(err)
	}
	if adapter.startCalls[2].Observability != nil {
		t.Fatal("Debug.Enabled=false must override HARNESS_DEBUG=1")
	}
}

func TestFormatDiagnosticForStderr(t *testing.T) {
	d := Diagnostic{Level: DebugLevelError, Subsystem: "bridge.turn", Message: "bad", Error: &DiagnosticError{Message: "oops"}}
	if got, want := formatDiagnosticForStderr(d), "[harness:error] bridge.turn bad (Error: oops)\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTraceTreeReporter_RendersTree(t *testing.T) {
	var out strings.Builder
	r := NewTraceTreeReporter(TraceTreeReporterOptions{Write: func(s string) { out.WriteString(s) }})
	ctx := context.Background()
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "c", OperationType: "ai.harness", ModelID: "m"})
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "c", StepNumber: 0})
	r.OnToolExecutionStart(ctx, telemetry.TelemetryToolCallStartEvent{CallID: "c", ToolName: "bash", ToolCallID: "t1"})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "c", ToolCallID: "t1", Error: errors.New("x")})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "c"})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c"})
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "" ||
		!strings.HasPrefix(lines[1], "- ai.harness m ") ||
		!strings.HasPrefix(lines[2], "  - step 1 ") ||
		!strings.HasPrefix(lines[3], "    - tool bash [error] ") ||
		!strings.HasSuffix(lines[3], "ms") {
		t.Fatalf("tree = %q", out.String())
	}
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c"}) // second end renders nothing
	if strings.Count(out.String(), "ai.harness") != 1 {
		t.Fatal("rendered twice")
	}
}

func TestFileReporter_StepFinishOutputIncludesToolResults(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	st := reporterSettings()
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "c", Settings: st})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{
		CallID: "c", Settings: st,
		ToolCalls: []types.ToolCall{{ID: "t1", ToolName: "bash", Arguments: map[string]any{"command": "ls"}}},
		ToolResults: []types.ToolResult{
			{ToolCallID: "t1", ToolName: "bash", Input: map[string]any{"command": "ls"}, Result: map[string]any{"ok": true}},
			{ToolCallID: "t2", ToolName: "bash", Error: errors.New("exit 1")},
		},
	})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c"})
	var out []any
	for _, l := range readEventLines(t, dir) {
		if l["kind"] == "step-finish" {
			out = l["output"].([]any)
		}
	}
	if len(out) != 3 {
		t.Fatalf("output = %v", out)
	}
	res := out[1].(map[string]any)
	if res["type"] != "tool-result" || res["toolCallId"] != "t1" || res["output"].(map[string]any)["ok"] != true {
		t.Fatalf("tool-result = %v", res)
	}
	te := out[2].(map[string]any)
	if te["type"] != "tool-error" || len(te["error"].(map[string]any)) != 0 {
		t.Fatalf("tool-error = %v", te)
	}
}
