package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func reporterRawLines(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// TS usage always carries inputTokenDetails and outputTokenDetails objects
// (asLanguageModelUsage), and provider / modelId are always strings, so an
// unknown harness model is written as "modelId":"".
func TestR47FileReporterUsageDetailsAndEmptyModelID(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	st := reporterSettings()
	in := int64(3)
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "c", OperationType: "ai.harness", ModelProvider: "harness:x", Settings: st})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "c", Settings: st, Usage: telemetry.TelemetryUsage{InputTokens: &in}})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c", FinishReason: "stop"})
	lines := reporterRawLines(t, dir)
	if len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.Contains(lines[0], `"operationId":"ai.harness","provider":"harness:x","modelId":"","input":`) {
		t.Fatalf("turn-start = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"usage":{"inputTokens":3,"inputTokenDetails":{},"outputTokenDetails":{}}`) {
		t.Fatalf("step-finish = %s", lines[1])
	}
	if !strings.Contains(lines[2], `"usage":{"inputTokenDetails":{},"outputTokenDetails":{}}`) {
		t.Fatalf("turn-finish = %s", lines[2])
	}
}

// TS tool-end output for a runtime-reported failure is the raw wire value
// (`error: value.result`); only a thrown Error writes `{}`. The same holds
// for a provider-executed tool-error in the step content.
func TestR47FileReporterToolErrorWritesRawWireValue(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	st := reporterSettings()
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "c", Settings: st})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "c", ToolCallID: "t1", Error: ErrorFromValue("permission denied")})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "c", ToolCallID: "t2", Error: ErrorFromValue(map[string]any{"code": float64(7)})})
	r.OnToolExecutionEnd(ctx, telemetry.TelemetryToolCallFinishEvent{CallID: "c", ToolCallID: "t3", Error: errors.New("thrown")})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "c", Settings: st, ToolResults: []types.ToolResult{
		{ToolCallID: "p1", ToolName: "web", Error: errors.New("upstream"), Result: map[string]any{"kind": "timeout"}, ProviderExecuted: true},
		{ToolCallID: "h1", ToolName: "host", Error: errors.New("thrown")},
	}})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c"})
	lines := reporterRawLines(t, dir)
	for i, want := range []string{
		`"toolCallId":"t1","isError":true,"output":"permission denied"}`,
		`"toolCallId":"t2","isError":true,"output":{"code":7}}`,
		`"toolCallId":"t3","isError":true,"output":{}}`,
		`"toolCallId":"p1","toolName":"web","input":null,"error":{"kind":"timeout"}}`,
	} {
		if !strings.Contains(lines[min(i+1, 4)], want) {
			t.Fatalf("line %d = %s, want %s", i, lines[min(i+1, 4)], want)
		}
	}
	if !strings.Contains(lines[4], `"toolCallId":"h1","toolName":"host","input":null,"error":{}}`) {
		t.Fatalf("step-finish = %s", lines[4])
	}
}

// End to end: a runtime tool-error result reaches OnToolExecutionEnd and the
// reporter with its wire value, and the callback's error text is
// getErrorMessage's (JSON for an object, not Go map formatting).
func TestR47RuntimeToolErrorKeepsWireValue(t *testing.T) {
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
				&ToolCallPart{ToolCallID: "n1", ToolName: "nativeLookup", Input: `{}`, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "n1", ToolName: "nativeLookup", Result: map[string]any{"code": float64(7)}, IsError: true, Dynamic: true},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var cbErr error
	tel := telemetry.DefaultSettings()
	tel.Integrations = []telemetry.TelemetryIntegration{reporter}
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, Telemetry: tel,
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			cbErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotErr := cbErr
	mu.Unlock()
	if gotErr == nil || gotErr.Error() != `{"code":7}` {
		t.Fatalf("callback error = %v", gotErr)
	}
	var toolEnd string
	for _, l := range reporterRawLines(t, dir) {
		if strings.Contains(l, `"kind":"tool-end"`) {
			toolEnd = l
		}
	}
	if !strings.Contains(toolEnd, `"isError":true,"output":{"code":7}}`) {
		t.Fatalf("tool-end = %q", toolEnd)
	}
}
