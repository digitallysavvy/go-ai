package harness

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r85carryBashTool() types.Tool {
	return types.Tool{Name: "bash", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}}}}
}

func r85carryTicketsTool(calls *int32, fails bool) types.Tool {
	return types.Tool{
		Name: "listProjectTickets", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(calls, 1)
			time.Sleep(10 * time.Millisecond)
			if fails {
				return nil, errors.New("Ticket lookup failed")
			}
			return []interface{}{map[string]interface{}{"id": "AI-23503"}}, nil
		},
	}
}

// Ports TS 6eab0f7b11 run-prompt.test.ts "pauses for repeated built-in
// approvals without a host result echo": a built-in approval blocks the
// runtime, so the echo of a host tool result submitted in the same message
// never arrives. The turn must still pause (it used to hang), report the
// host result as a completed pending result with the call's provider
// metadata, surface its outcome once, and on the next pause report the same
// pending result again without running the tool again.
func TestR85carryBuiltinApprovalPausesWithoutHostEcho(t *testing.T) {
	for _, tc := range []struct{ approved, fails bool }{{true, false}, {false, false}, {true, true}, {false, true}} {
		t.Run("", func(t *testing.T) {
			var calls int32
			bashCall := &ToolCallPart{ToolCallID: "bash-1", ToolName: "bash", NativeName: "Bash", Input: `{"command":"pwd"}`, ProviderExecuted: true}
			hostCall := &ToolCallPart{ToolCallID: "host-1", ToolName: "listProjectTickets", Input: `{}`, ProviderMetadata: ProviderMetadata{"test": {"source": "original-call"}}}
			approvalRequest := &ToolApprovalRequestPart{ApprovalID: "bash-1", ToolCallID: "bash-1"}
			var mu sync.Mutex
			var submitted []ToolResultSubmission
			record := func(s ToolResultSubmission) { mu.Lock(); submitted = append(submitted, s); mu.Unlock() }
			builtin := map[string]BuiltinTool{"bash": {Tool: r85carryBashTool(), NativeName: "Bash"}}
			// The runtime keeps the turn open while it waits for the
			// approval, as Claude Code does.
			hold := make(chan struct{})
			t.Cleanup(func() { close(hold) })
			holdTurn := func() <-chan struct{} { return hold }
			first := newMockHarness(mockHarnessOptions{
				builtinTools: builtin, supportsApproval: true, onSubmitResult: record, promptDone: holdTurn,
				script: func(func(string, interface{})) []StreamPart {
					return []StreamPart{&StreamStartPart{ModelID: "fake-model"}, bashCall, hostCall, approvalRequest, hostInputBoundary(7)}
				},
			})
			tickets := r85carryTicketsTool(&calls, tc.fails)
			tools := map[string]types.Tool{"bash": r85carryBashTool(), "listProjectTickets": tickets}
			active := map[string]types.Tool{"listProjectTickets": tickets}
			var pendingApprovals []PendingToolApproval
			var pendingResults []PendingToolResult
			out := runPrompt(context.Background(), runPromptInput{
				Harness: first.harness, Session: first.session, Prompt: TextPrompt("Run Bash and list the project tickets."), SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: active,
				ToolApproval:          ToolApprovalConfiguration{"listProjectTickets": ai.ToolApprovalStatusApproved},
				OnPendingToolApproval: func(a PendingToolApproval) { pendingApprovals = append(pendingApprovals, a) },
				OnPendingToolResult:   func(r PendingToolResult) { pendingResults = append(pendingResults, r) },
			})
			done := make(chan []provider.StreamChunk)
			go func() { done <- drainRunPrompt(t, out) }()
			var chunks []provider.StreamChunk
			select {
			case chunks = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the turn never paused (stream hang)")
			}

			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Fatalf("execute calls = %d, want 1", n)
			}
			mu.Lock()
			if len(submitted) != 1 {
				t.Fatalf("submitted = %d, want 1", len(submitted))
			}
			firstSubmission := submitted[0]
			mu.Unlock()
			if len(pendingApprovals) != 1 || pendingApprovals[0].ApprovalID != "bash-1" || pendingApprovals[0].Kind != PendingToolApprovalBuiltin {
				t.Fatalf("pending approvals = %+v", pendingApprovals)
			}
			if len(pendingResults) != 1 || pendingResults[0].ToolCallID != "host-1" || pendingResults[0].CompletedResult == nil ||
				!reflect.DeepEqual(pendingResults[0].CompletedResult.Output, firstSubmission.Output) ||
				!reflect.DeepEqual(pendingResults[0].ProviderOptions, map[string]map[string]any{"test": {"source": "original-call"}}) {
				t.Fatalf("pending results = %+v", pendingResults)
			}
			if n := len(chunksOfType(chunks, provider.ChunkTypeToolApprovalRequest)); n != 1 {
				t.Fatalf("approval requests = %d, want 1", n)
			}
			results := chunksOfType(chunks, provider.ChunkTypeToolResult)
			if len(results) != 1 || results[0].ToolResult.ToolCallID != "host-1" || (results[0].ToolResult.Error != nil) != tc.fails {
				t.Fatalf("tool results = %+v, want one host-1 (error %v)", results, tc.fails)
			}
			steps := out.Result.Steps()
			if len(steps) != 1 || steps[0].FinishReason != types.FinishReasonToolCalls {
				t.Fatalf("steps = %+v", steps)
			}
			if in := steps[0].Usage.InputTokens; in == nil || *in != 7 {
				t.Fatalf("usage = %+v, want 7", steps[0].Usage)
			}
			wantContent := []string{"tool-call", "tool-call", "tool-approval-request", "tool-result"}
			if tc.fails {
				wantContent[3] = "tool-error"
			}
			if got := contentTypes(steps[0]); !reflect.DeepEqual(got, wantContent) {
				t.Fatalf("content = %v, want %v", got, wantContent)
			}

			// Second slice: the bash-1 answer is carried out, the runtime asks
			// for another built-in approval, and host-1 is still not echoed.
			second := newMockHarness(mockHarnessOptions{
				builtinTools: builtin, supportsApproval: true, onSubmitResult: record, promptDone: holdTurn,
				continueScript: func(func(string, interface{})) []StreamPart {
					bash2 := *bashCall
					bash2.ToolCallID = "bash-2"
					return []StreamPart{&bash2, &ToolApprovalRequestPart{ApprovalID: "bash-2", ToolCallID: "bash-2"}, hostInputBoundary(3)}
				},
			})
			var nextApprovals []PendingToolApproval
			var nextResults []PendingToolResult
			out2 := runPrompt(context.Background(), runPromptInput{
				Harness: second.harness, Session: second.session, Mode: "continue", SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: active,
				PendingToolApprovals:      pendingApprovals,
				PendingToolResults:        pendingResults,
				ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "bash-1", ToolCallID: "bash-1", Approved: tc.approved}},
				OnPendingToolApproval:     func(a PendingToolApproval) { nextApprovals = append(nextApprovals, a) },
				OnPendingToolResult:       func(r PendingToolResult) { nextResults = append(nextResults, r) },
			})
			go func() { done <- drainRunPrompt(t, out2) }()
			var chunks2 []provider.StreamChunk
			select {
			case chunks2 = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the continued turn never paused")
			}
			if !reflect.DeepEqual(nextResults, pendingResults) {
				t.Fatalf("next pending results = %+v, want the same as %+v", nextResults, pendingResults)
			}
			if len(nextApprovals) != 1 || nextApprovals[0].ApprovalID != "bash-2" || nextApprovals[0].Kind != PendingToolApprovalBuiltin {
				t.Fatalf("next approvals = %+v", nextApprovals)
			}
			for _, c := range chunks2 {
				if (c.ToolCall != nil && c.ToolCall.ID == "host-1") || (c.ToolResult != nil && c.ToolResult.ToolCallID == "host-1") {
					t.Fatalf("host-1 surfaced again: %+v", c)
				}
			}
			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Fatalf("execute calls = %d, want 1", n)
			}
			mu.Lock()
			n := len(submitted)
			mu.Unlock()
			if n != 2 {
				t.Fatalf("submitted = %d, want 2 (the completed result resubmitted)", n)
			}
		})
	}
}

// Ports TS 033f8d9a63 denied-tool-output.test.ts ("configured" and
// "filtered" sources, echo on and off, with a converter): every denial
// submission carries a model tool result with the execution-denied output,
// the tool and its ToModelOutput never run, and an echoed denial reaches the
// response messages as the execution-denied output.
func TestR85carryDeniedHostToolOutput(t *testing.T) {
	const filteredReason = "Tool 'weather' is inactive due to the HarnessAgent tool filtering policy."
	for _, source := range []string{"configured", "filtered"} {
		for _, echo := range []bool{false, true} {
			reasons := []string{"weather disabled", ""}
			if source == "filtered" {
				reasons = []string{filteredReason}
			}
			for _, reason := range reasons {
				t.Run(source, func(t *testing.T) {
					var executed, converted atomic.Int32
					weather := types.Tool{
						Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
						Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
							executed.Add(1)
							return map[string]any{"content": []any{map[string]any{"text": "Sunny"}}}, nil
						},
						ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
							converted.Add(1)
							return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Invalid weather result"}, nil
						},
					}
					stop := FinishReason{Unified: FinishReasonStop, Raw: "end_turn"}
					mock := newScriptedHarness(
						func(emit func(StreamPart)) {
							emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`})
						},
						func(sub ToolResultSubmission, emit func(StreamPart)) {
							if echo {
								emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "weather", Result: sub.Output})
							}
							emit(&TextStartPart{ID: "answer"})
							emit(&TextDeltaPart{ID: "answer", Delta: "Weather is disabled."})
							emit(&TextEndPart{ID: "answer"})
							emit(&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)})
							emit(&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)})
						},
					)
					tools := map[string]types.Tool{"weather": weather}
					in := runPromptInput{
						Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("Get the weather"), SandboxSession: testSandbox(),
						SessionWorkDir: "/sandbox", Tools: tools, ActiveTools: tools,
					}
					if source == "filtered" {
						in.ActiveTools = map[string]types.Tool{}
					} else {
						in.ToolApproval = ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusDenied}
						in.ToolApprovalReasons = map[string]string{"weather": reason}
					}
					out := runPrompt(context.Background(), in)
					drainRunPrompt(t, out)
					if err := out.Result.Err(); err != nil {
						t.Fatalf("turn failed: %v", err)
					}

					denial := map[string]any{"type": "execution-denied"}
					if reason != "" {
						denial["reason"] = reason
					}
					if len(mock.toolResults) != 1 {
						t.Fatalf("submissions = %+v", mock.toolResults)
					}
					got := r85carryJSON(t, map[string]any{"toolCallId": mock.toolResults[0].ToolCallID, "output": mock.toolResults[0].Output, "toolResult": mock.toolResults[0].ToolResult})
					want := r85carryJSON(t, map[string]any{"toolCallId": "c1", "output": denial, "toolResult": map[string]any{"type": "tool-result", "toolCallId": "c1", "toolName": "weather", "output": denial}})
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("submission = %v, want %v", got, want)
					}
					if executed.Load() != 0 || converted.Load() != 0 {
						t.Fatalf("executed=%d converted=%d, want 0/0", executed.Load(), converted.Load())
					}
					var outputs []any
					for _, m := range out.Result.ResponseMessages() {
						if m.Role != types.RoleTool {
							continue
						}
						for _, c := range m.Content {
							if tr, ok := c.(types.ToolResultContent); ok && tr.Output != nil {
								outputs = append(outputs, r85carryJSON(t, tr.Output))
							}
						}
					}
					var wantOutputs []any
					if source == "configured" {
						wantOutputs = append(wantOutputs, r85carryJSON(t, denial))
					}
					if echo {
						wantOutputs = append(wantOutputs, r85carryJSON(t, denial))
					}
					if !reflect.DeepEqual(outputs, wantOutputs) {
						t.Fatalf("echo=%v reason=%q: response tool outputs = %v, want %v", echo, reason, outputs, wantOutputs)
					}
				})
			}
		}
	}
}

func r85carryJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Ports TS e7a8988893 "preserves $nativeName in built-in tool approval
// metadata": the parsed tool call (stream tool-call, approval request and
// step) carries providerMetadata[harnessId].nativeToolName when the native
// name differs, merged over the harness's existing metadata; a native name
// equal to the tool name adds nothing.
func TestR85carryNativeToolNameMetadata(t *testing.T) {
	for _, tc := range []struct{ toolName, nativeName string }{{"bash", "Bash"}, {"write", "Write"}, {"edit", "Edit"}, {"Bash", "Bash"}} {
		t.Run(tc.toolName+"/"+tc.nativeName, func(t *testing.T) {
			metadata := ProviderMetadata{"mock": {"session": "s1"}, "other": {"k": "v"}}
			mock := newMockHarness(mockHarnessOptions{
				builtinTools: map[string]BuiltinTool{tc.toolName: {Tool: r85carryBashTool()}}, supportsApproval: true,
				script: func(func(string, interface{})) []StreamPart {
					return []StreamPart{
						&StreamStartPart{},
						&ToolCallPart{ToolCallID: "c1", ToolName: tc.toolName, Input: `{"command":"echo hello"}`, ProviderExecuted: true, NativeName: tc.nativeName, ProviderMetadata: metadata},
						&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
					}
				},
			})
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("Run the shell command `echo hello`"), SandboxSession: testSandbox(),
				Tools: map[string]types.Tool{tc.toolName: r85carryBashTool()},
			})
			chunks := drainRunPrompt(t, out)
			want := map[string]any{"mock": map[string]any{"session": "s1"}, "other": map[string]any{"k": "v"}}
			if tc.nativeName != tc.toolName {
				want["mock"] = map[string]any{"session": "s1", "nativeToolName": tc.nativeName}
			}
			calls := chunksOfType(chunks, provider.ChunkTypeToolCall)
			if len(calls) != 1 || !reflect.DeepEqual(r85carryJSON(t, calls[0].ToolCall.ProviderMetadata), r85carryJSON(t, want)) {
				t.Fatalf("tool-call metadata = %+v, want %v", calls, want)
			}
			reqs := chunksOfType(chunks, provider.ChunkTypeToolApprovalRequest)
			if len(reqs) != 1 || !reflect.DeepEqual(r85carryJSON(t, reqs[0].ToolApprovalRequest.ToolCall.ProviderMetadata), r85carryJSON(t, want)) {
				t.Fatalf("approval request = %+v, want metadata %v", reqs, want)
			}
			if _, mutated := metadata["mock"]["nativeToolName"]; mutated {
				t.Fatal("the adapter's provider metadata was mutated")
			}
		})
	}
}
