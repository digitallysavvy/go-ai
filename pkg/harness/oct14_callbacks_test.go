package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the callback half of TS telemetry-integration.test.ts 'drives the
// Telemetry lifecycle from the turn stream' and 'uses final-step text and
// reasoning on the telemetry end event': every lifecycle callback fires with
// a correlating callId, the language-model-call-end event carries the step's
// content, provider metadata and response id, and Callbacks.OnEnd fires once
// with the final step and total usage.
func TestAgent_LifecycleCallbacksCarryTSEventData(t *testing.T) {
	var mu sync.Mutex
	callIDs := map[string]bool{}
	note := func(id string) { mu.Lock(); callIDs[id] = true; mu.Unlock() }
	var order []string
	add := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	var lmEnd ai.LanguageModelCallEndEvent
	var end ai.OnFinishEvent
	var start ai.OnStartEvent
	var endCalls int

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "draft"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "tool_calls"}, Usage: stringUsage(1, 1)},
				&TextStartPart{ID: "t2"}, &TextDeltaPart{ID: "t2", Delta: "final"}, &TextEndPart{ID: "t2"},
				&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: `{"command":"ls"}`, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: map[string]any{"output": "ok"}, Dynamic: true},
				&FinishStepPart{
					FinishReason:    FinishReason{Unified: FinishReasonStop, Raw: "stop"},
					Usage:           stringUsage(5, 2),
					HarnessMetadata: Metadata{"gateway": {"generationId": "generation-id"}},
				},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop, Raw: "stop"}, TotalUsage: stringUsage(6, 3)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Callbacks: Callbacks{
		OnStart: func(_ context.Context, e ai.OnStartEvent) { add("start"); note(e.CallID); start = e },
		OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) {
			add("stepStart")
			note(e.CallID)
		},
		OnLanguageModelCallStart: func(_ context.Context, e ai.LanguageModelCallStartEvent) {
			add("lmStart")
			note(e.CallID)
		},
		OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) {
			add("lmEnd")
			note(e.CallID)
			mu.Lock()
			lmEnd = e
			mu.Unlock()
		},
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) { add("toolStart"); note(e.CallID) },
		OnToolExecutionEnd:   func(_ context.Context, e ai.OnToolCallFinishEvent) { add("toolEnd"); note(e.CallID) },
		OnStepEnd:            func(_ context.Context, e ai.OnStepFinishEvent) { add("stepEnd"); note(e.CallID) },
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
			add("end")
			note(e.CallID)
			mu.Lock()
			end = e
			endCalls++
			mu.Unlock()
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if endCalls != 1 {
		t.Fatalf("OnEnd called %d times, want 1 (order %v)", endCalls, order)
	}
	if order[len(order)-1] != "end" {
		t.Fatalf("OnEnd is not last: %v", order)
	}
	if len(callIDs) != 1 || callIDs[""] {
		t.Fatalf("callbacks do not share one non-empty callId: %v", callIDs)
	}
	if start.OperationID != "ai.harness" || start.Provider == "" || len(start.Messages) != 1 {
		t.Fatalf("start event = %+v", start)
	}
	if lmEnd.ResponseID != lmEnd.CallID+"-1" {
		t.Fatalf("ResponseID = %q, want %q", lmEnd.ResponseID, lmEnd.CallID+"-1")
	}
	if gw, _ := lmEnd.ProviderMetadata["gateway"].(map[string]any); gw["generationId"] != "generation-id" {
		t.Fatalf("ProviderMetadata = %v", lmEnd.ProviderMetadata)
	}
	var sawText, sawCall, sawResult bool
	for _, p := range lmEnd.Content {
		switch c := p.(type) {
		case types.TextContent:
			sawText = c.Text == "final"
		case types.ToolCallContent:
			sawCall = c.ToolCallID == "c1" && c.ProviderExecuted
		case types.ToolResultContent:
			sawResult = c.ToolCallID == "c1"
		}
	}
	if !sawText || !sawCall || !sawResult {
		t.Fatalf("model-call content missing parts (text=%v call=%v result=%v): %#v", sawText, sawCall, sawResult, lmEnd.Content)
	}
	if end.Text != "final" || len(end.Steps) != 2 || end.StepNumber != 1 {
		t.Fatalf("end event text=%q steps=%d step=%d", end.Text, len(end.Steps), end.StepNumber)
	}
	if end.FinishReason != types.FinishReasonStop || end.RawFinishReason != "stop" {
		t.Fatalf("end finish = %v/%q", end.FinishReason, end.RawFinishReason)
	}
	if end.TotalUsage.InputTokens == nil || *end.TotalUsage.InputTokens != 6 {
		t.Fatalf("end total usage = %+v", end.TotalUsage)
	}
	if len(end.ToolCalls) != 1 || end.ToolCalls[0].ID != "c1" || len(end.ToolResults) != 1 {
		t.Fatalf("end tool calls/results = %v / %v", end.ToolCalls, end.ToolResults)
	}
	if gw, _ := end.ProviderMetadata["gateway"].(map[string]any); gw == nil {
		t.Fatalf("end ProviderMetadata = %v", end.ProviderMetadata)
	}
}
