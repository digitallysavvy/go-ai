package harness

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// r72 stream CR-3: a harness turn's OnStepEnd event carries the recorded
// step's content, performance and request messages (TS onStepEnd receives
// the StepResult).
func TestR72IntegHarnessStepEndCarriesContent(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: `{"command":"ls"}`, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: map[string]any{"output": "ok"}, Dynamic: true},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var events []ai.OnStepFinishEvent
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Callbacks: Callbacks{
		OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
	}})
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
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || len(res.Steps) != 1 {
		t.Fatalf("events = %d, steps = %d", len(events), len(res.Steps))
	}
	e, step := events[0], res.Steps[0]
	if len(e.Content) != 2 || !reflect.DeepEqual(e.Content, step.Content) {
		t.Errorf("content = %#v, want %#v", e.Content, step.Content)
	}
	if !reflect.DeepEqual(e.Performance, step.Performance) {
		t.Errorf("performance = %#v, want %#v", e.Performance, step.Performance)
	}
	if !reflect.DeepEqual(e.Request.Messages, step.Request.Messages) {
		t.Errorf("request messages = %#v, want %#v", e.Request.Messages, step.Request.Messages)
	}
}
