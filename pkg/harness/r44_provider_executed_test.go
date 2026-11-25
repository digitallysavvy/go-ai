package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS records a custom (host tool) approval with `providerExecuted: false`.
// The record is persisted in lifecycle state, and when the approval continues,
// the rebuilt tool call copies the explicit false.
func TestR44CustomPendingApprovalRecordsProviderExecutedFalse(t *testing.T) {
	weather := types.Tool{
		Name:       "weather",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
	tools := map[string]types.Tool{"weather": weather}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{}`},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var recorded []PendingToolApproval
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
		OnPendingToolApproval: func(a PendingToolApproval) {
			mu.Lock()
			recorded = append(recorded, a)
			mu.Unlock()
		},
	})
	drainRunPrompt(t, out)
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 1 {
		t.Fatalf("pending approvals = %+v, want 1", recorded)
	}
	a := recorded[0]
	if a.Kind != PendingToolApprovalCustom || a.ProviderExecuted == nil || *a.ProviderExecuted {
		t.Fatalf("pending approval = %+v, want kind custom with providerExecuted false", a)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"providerExecuted":false`) {
		t.Fatalf("pending approval JSON = %s, want providerExecuted:false", raw)
	}

	// Continuing the recorded approval: the approval response's tool call
	// keeps the explicit false.
	cont := newMockHarness(mockHarnessOptions{})
	out = runPrompt(context.Background(), runPromptInput{
		Harness: cont.harness, Session: cont.session, Mode: "continue", SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		PendingToolApprovals:      []PendingToolApproval{a},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: a.ApprovalID, ToolCallID: "c1", Approved: true}},
	})
	chunks := drainRunPrompt(t, out)
	responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
	if len(responses) != 1 {
		t.Fatalf("approval responses = %+v, want 1", responses)
	}
	r := responses[0].ToolApprovalResponse
	if r.ProviderExecuted || r.ToolCall.ProviderExecuted || !r.ToolCall.ProviderExecutedSet {
		t.Fatalf("approval response = %+v, want tool call providerExecuted false (set)", r)
	}
}

// A call to a tool outside the active set is denied by the host. TS sets the
// execution's toolOutput to that denial right away, so the
// tool-execution-end callback reports the host's output, not the runtime's
// echoed copy.
func TestR44InactiveToolDenialReportsHostOutput(t *testing.T) {
	hidden := types.Tool{
		Name:       "hidden",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ran", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "hidden", Input: `{}`},
				// The runtime echoes its own rendering of the denial.
				&ToolResultPart{ToolCallID: "c1", ToolName: "hidden", Result: "denied by host"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var ends []ai.OnToolCallFinishEvent
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: map[string]types.Tool{"hidden": hidden}, ActiveTools: map[string]types.Tool{},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			ends = append(ends, e)
			mu.Unlock()
		}},
	})
	drainRunPrompt(t, out)
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("tool execution ends = %+v, want 1", ends)
	}
	got, ok := ends[0].ToolOutput.Result.(map[string]interface{})
	if !ok || got["type"] != "execution-denied" || got["reason"] != BuiltinToolFilteringDenialReason("hidden") {
		t.Fatalf("tool execution end output = %#v, want the host's execution-denied output", ends[0].ToolOutput.Result)
	}
}

// A continued builtin approval's tool call is skipped as a settled replay,
// so its real tool-result arrives without a tracked execution. TS publishes
// tool-execution callbacks only for tracked executions (`if (execution !=
// null)`), so neither the start nor the end fires for it.
func TestR44UntrackedToolResultFiresNoExecutionCallbacks(t *testing.T) {
	yes := true
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				mustDecodePart(t, `{"type":"tool-call","toolCallId":"c1","toolName":"bash","input":"{}","providerExecuted":true,"dynamic":true}`),
				&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: "done"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var events []string
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue", SandboxSession: testSandbox(),
		PendingToolApprovals: []PendingToolApproval{{
			ApprovalID: "a1", ToolCallID: "c1", ToolName: "bash", Input: `{}`,
			Kind: PendingToolApprovalBuiltin, ProviderExecuted: &yes,
		}},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
		Callbacks: Callbacks{
			OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
				mu.Lock()
				events = append(events, "start:"+e.ToolCallID)
				mu.Unlock()
			},
			OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
				mu.Lock()
				events = append(events, "end:"+e.ToolCallID)
				mu.Unlock()
			},
		},
	})
	chunks := drainRunPrompt(t, out)
	<-out.Done
	if results := chunksOfType(chunks, provider.ChunkTypeToolResult); len(results) != 1 {
		t.Fatalf("tool results = %+v, want the runtime's result", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 {
		t.Fatalf("tool execution callbacks = %v, want none", events)
	}
}

// A host tool whose result the runtime never echoes still reports its
// tool-execution end when the step closes (TS publishToolExecutions at
// completeStep).
func TestR44UnechoedHostToolPublishesEndAtStepClose(t *testing.T) {
	echo := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	tools := map[string]types.Tool{"echo": echo}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "echo", Input: `{}`},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var ends []ai.OnToolCallFinishEvent
	stepEnded := false
	endBeforeStep := false
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		Callbacks: Callbacks{
			OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
				mu.Lock()
				ends = append(ends, e)
				endBeforeStep = !stepEnded
				mu.Unlock()
			},
			OnStepEnd: func(context.Context, ai.OnStepFinishEvent) {
				mu.Lock()
				stepEnded = true
				mu.Unlock()
			},
		},
	})
	drainRunPrompt(t, out)
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 || ends[0].ToolOutput.Result != "ok" || !endBeforeStep {
		t.Fatalf("tool execution ends = %+v (before step end: %v), want one with the host output before the step ends", ends, endBeforeStep)
	}
}

// TS keeps the display (work-dir-stripped) tool-call event in
// rawToolCallsByToolCallId, so a builtin approval record and the tool call of
// its approval response carry the stripped input, never the absolute path.
func TestR44BuiltinApprovalUsesStrippedInput(t *testing.T) {
	for _, continued := range []bool{false, true} {
		mock := newMockHarness(mockHarnessOptions{
			script: func(submit func(string, interface{})) []StreamPart {
				stop := FinishReason{Unified: FinishReasonStop}
				return []StreamPart{
					&StreamStartPart{},
					mustDecodePart(t, `{"type":"tool-call","toolCallId":"c1","toolName":"bash","input":"{\"path\":\"/work/dir/a.txt\"}","providerExecuted":true,"dynamic":true}`),
					&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
					&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
					&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
				}
			},
		})
		var mu sync.Mutex
		var recorded []PendingToolApproval
		in := runPromptInput{
			Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
			SessionWorkDir: "/work/dir",
			OnPendingToolApproval: func(a PendingToolApproval) {
				mu.Lock()
				recorded = append(recorded, a)
				mu.Unlock()
			},
		}
		if continued {
			in.ToolApprovalContinuations = []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}}
		}
		chunks := drainRunPrompt(t, runPrompt(context.Background(), in))
		if continued {
			responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
			if len(responses) != 1 || responses[0].ToolApprovalResponse.ToolCall.Arguments["path"] != "a.txt" {
				t.Fatalf("approval responses = %+v, want the stripped path", responses)
			}
			continue
		}
		mu.Lock()
		if len(recorded) != 1 || recorded[0].Input != `{"path":"a.txt"}` {
			t.Fatalf("pending approvals = %+v, want the stripped input", recorded)
		}
		mu.Unlock()
	}
}

// A host tool that fails after the host suspended the turn is published as a
// tool-error (TS enqueueHostToolOutcome), not as a successful tool-result.
func TestR44SuspendedHostToolFailureIsToolError(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	tool := types.Tool{
		Name: "research", Description: "r", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-release
			return nil, errors.New("boom")
		},
	}
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &ToolCallPart{ToolCallID: "call_1", ToolName: "research", Input: "{}"}}
		},
		promptDone: func() <-chan struct{} {
			select {
			case <-finish:
				return nil
			default:
				return finish
			}
		},
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			once.Do(func() { close(suspendCalled); close(finish) })
			return NewContinueTurnState("mock", map[string]any{})
		},
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{"research": tool}, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() {
		_, err := session.SuspendTurn(context.Background())
		done <- err
	}()
	<-suspendCalled
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var results []types.ToolResult
	stream := res.Stream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			results = append(results, *c.ToolResult)
		}
	}
	if len(results) != 1 || results[0].Error == nil || !strings.Contains(results[0].Error.Error(), "boom") {
		t.Fatalf("tool results = %+v, want one tool-error carrying the failure", results)
	}
}
