package harness

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r88carryWriteTool(calls *int32) types.Tool {
	return types.Tool{
		Name:       "write",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(calls, 1)
			return map[string]interface{}{"value": args["value"]}, nil
		},
	}
}

func r88carryAwait(t *testing.T, out *runPromptOutput, what string) []provider.StreamChunk {
	t.Helper()
	done := make(chan []provider.StreamChunk, 1)
	go func() { done <- drainRunPrompt(t, out) }()
	select {
	case c := <-done:
		return c
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never paused (stream hang)", what)
	}
	return nil
}

// Ports TS 8e8033a73d run-prompt.test.ts "pauses for a host approval before
// waiting for another host result echo": a custom host-tool approval can
// block the runtime from echoing another host tool's result, so the turn
// pauses at the step end without that echo (it used to wait forever).
func TestR88carryHostApprovalPausesWithoutHostEcho(t *testing.T) {
	var gatedCalls, allowedCalls int32
	gated := r88carryWriteTool(&gatedCalls)
	gated.Name = "gated_write"
	allowed := r88carryWriteTool(&allowedCalls)
	allowed.Name = "allowed_write"
	var mu sync.Mutex
	var submitted []ToolResultSubmission
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	m := newMockHarness(mockHarnessOptions{
		onSubmitResult: func(s ToolResultSubmission) { mu.Lock(); submitted = append(submitted, s); mu.Unlock() },
		promptDone:     func() <-chan struct{} { return hold },
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{ModelID: "fake-model"},
				&ToolCallPart{ToolCallID: "gated-1", ToolName: "gated_write", Input: `{"value":"gated"}`},
				&ToolCallPart{ToolCallID: "allowed-1", ToolName: "allowed_write", Input: `{"value":"allowed"}`},
				hostInputBoundary(6),
			}
		},
	})
	tools := map[string]types.Tool{"gated_write": gated, "allowed_write": allowed}
	var approvals []PendingToolApproval
	var results []PendingToolResult
	out := runPrompt(context.Background(), runPromptInput{
		Harness: m.harness, Session: m.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval:          ToolApprovalConfiguration{"gated_write": ai.ToolApprovalStatusUserApproval, "allowed_write": ai.ToolApprovalStatusApproved},
		OnPendingToolApproval: func(a PendingToolApproval) { approvals = append(approvals, a) },
		OnPendingToolResult:   func(r PendingToolResult) { results = append(results, r) },
	})
	chunks := r88carryAwait(t, out, "the turn")

	if atomic.LoadInt32(&gatedCalls) != 0 || atomic.LoadInt32(&allowedCalls) != 1 {
		t.Fatalf("executes: gated %d, allowed %d", gatedCalls, allowedCalls)
	}
	mu.Lock()
	if len(submitted) != 1 || submitted[0].ToolCallID != "allowed-1" || !reflect.DeepEqual(submitted[0].Output, map[string]interface{}{"value": "allowed"}) {
		t.Fatalf("submitted = %+v", submitted)
	}
	mu.Unlock()
	if len(approvals) != 1 || approvals[0].ToolCallID != "gated-1" || approvals[0].ToolName != "gated_write" || approvals[0].Kind != PendingToolApprovalCustom {
		t.Fatalf("approvals = %+v", approvals)
	}
	if len(results) != 1 || results[0].ToolCallID != "allowed-1" || results[0].ToolName != "allowed_write" ||
		results[0].Input != `{"value":"allowed"}` || results[0].CompletedResult == nil ||
		!reflect.DeepEqual(results[0].CompletedResult.Output, map[string]interface{}{"value": "allowed"}) {
		t.Fatalf("pending results = %+v", results)
	}
	if n := len(chunksOfType(chunks, provider.ChunkTypeToolApprovalRequest)); n != 1 {
		t.Fatalf("approval requests = %d", n)
	}
	res := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(res) != 1 || res[0].ToolResult.ToolCallID != "allowed-1" {
		t.Fatalf("tool results = %+v", res)
	}
}

// Ports TS 8e8033a73d harness-agent.test.ts "retains a host approval result
// across a subsequent approval pause and session resume" (run-prompt level):
// after an approved host call runs, a later approval pause in the same turn
// still reports its result as a completed pending result (TS keeps the raw
// call in rawToolCallsByToolCallId), so a resumed session can resubmit it
// without running the tool again.
func TestR88carryApprovedHostResultSurvivesNextApprovalPause(t *testing.T) {
	var firstCalls, secondCalls int32
	first := r88carryWriteTool(&firstCalls)
	first.Name = "first_write"
	second := r88carryWriteTool(&secondCalls)
	second.Name = "second_write"
	tools := map[string]types.Tool{"first_write": first, "second_write": second}
	approval := ToolApprovalConfiguration{"first_write": ai.ToolApprovalStatusUserApproval, "second_write": ai.ToolApprovalStatusUserApproval}

	var mu sync.Mutex
	var submitted []ToolResultSubmission
	record := func(s ToolResultSubmission) { mu.Lock(); submitted = append(submitted, s); mu.Unlock() }
	m1 := newMockHarness(mockHarnessOptions{onSubmitResult: record,
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&ToolCallPart{ToolCallID: "host-1", ToolName: "first_write", Input: `{"value":"first"}`}, hostInputBoundary(0)}
		},
	})
	var approvals1 []PendingToolApproval
	out1 := runPrompt(context.Background(), runPromptInput{
		Harness: m1.harness, Session: m1.session, Prompt: TextPrompt("Write both values."), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools, ToolApproval: approval,
		OnPendingToolApproval: func(a PendingToolApproval) { approvals1 = append(approvals1, a) },
	})
	r88carryAwait(t, out1, "the first turn")
	if len(approvals1) != 1 || approvals1[0].ToolCallID != "host-1" {
		t.Fatalf("first approvals = %+v", approvals1)
	}

	// The runtime holds the host-1 echo while it asks about host-2.
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	m2 := newMockHarness(mockHarnessOptions{onSubmitResult: record, promptDone: func() <-chan struct{} { return hold },
		continueScript: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&ToolCallPart{ToolCallID: "host-2", ToolName: "second_write", Input: `{"value":"second"}`}, hostInputBoundary(0)}
		},
	})
	var approvals2 []PendingToolApproval
	var results2 []PendingToolResult
	out2 := runPrompt(context.Background(), runPromptInput{
		Harness: m2.harness, Session: m2.session, Mode: "continue", SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools, ToolApproval: approval,
		PendingToolApprovals:      approvals1,
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: approvals1[0].ApprovalID, ToolCallID: "host-1", Approved: true}},
		OnPendingToolApproval:     func(a PendingToolApproval) { approvals2 = append(approvals2, a) },
		OnPendingToolResult:       func(r PendingToolResult) { results2 = append(results2, r) },
	})
	r88carryAwait(t, out2, "the continued turn")

	if atomic.LoadInt32(&firstCalls) != 1 || atomic.LoadInt32(&secondCalls) != 0 {
		t.Fatalf("executes: first %d, second %d", firstCalls, secondCalls)
	}
	mu.Lock()
	if len(submitted) != 1 || submitted[0].ToolCallID != "host-1" {
		t.Fatalf("submitted = %+v", submitted)
	}
	mu.Unlock()
	if len(approvals2) != 1 || approvals2[0].ToolCallID != "host-2" || approvals2[0].Kind != PendingToolApprovalCustom {
		t.Fatalf("second approvals = %+v", approvals2)
	}
	if len(results2) != 1 || results2[0].ToolCallID != "host-1" || results2[0].ToolName != "first_write" ||
		results2[0].Input != `{"value":"first"}` || results2[0].CompletedResult == nil ||
		!reflect.DeepEqual(results2[0].CompletedResult.Output, map[string]interface{}{"value": "first"}) {
		t.Fatalf("pending results = %+v", results2)
	}
}
