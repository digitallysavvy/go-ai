package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Owner ruling r71: a failed tool call is not one of the step's ToolResults
// (TS toolResults = tool-result parts), but the harness telemetry step end
// still reports it, so the file reporter's step-finish content keeps the
// tool-error part (TS reporter writes step.content).
func TestR71IntegHarnessStepEndKeepsToolError(t *testing.T) {
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
				&ToolCallPart{ToolCallID: "n2", ToolName: "nativeLookup", Input: `{}`, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "n2", ToolName: "nativeLookup", Result: "ok", Dynamic: true},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tel := telemetry.DefaultSettings()
	tel.Integrations = []telemetry.TelemetryIntegration{reporter}
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Telemetry: tel})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].ToolCallID != "n2" {
		t.Errorf("ToolResults = %+v, want only n2", res.ToolResults)
	}
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}
	var stepFinish string
	for _, l := range reporterRawLines(t, dir) {
		if strings.Contains(l, `"kind":"step-finish"`) {
			stepFinish = l
		}
	}
	if !strings.Contains(stepFinish, `"type":"tool-error","toolCallId":"n1"`) || !strings.Contains(stepFinish, `"type":"tool-result","toolCallId":"n2"`) {
		t.Fatalf("step-finish = %s, want the tool-error and the tool-result", stepFinish)
	}
}
