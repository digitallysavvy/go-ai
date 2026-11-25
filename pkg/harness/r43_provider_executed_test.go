package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func mustDecodePart(t *testing.T, frame string) StreamPart {
	t.Helper()
	p, err := DecodeStreamPart([]byte(frame))
	if err != nil {
		t.Fatalf("DecodeStreamPart(%s): %v", frame, err)
	}
	return p
}

// TS forwards tool-input-start unchanged and validateToolCall copies
// `providerExecuted` whenever the event defines it, so an explicit false from
// a runtime reaches the consumer (types.ToolCall.ProviderExecutedSet) while
// an omitted key does not.
func TestR43RunPromptKeepsExplicitProviderExecutedFalse(t *testing.T) {
	echo := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	tools := map[string]types.Tool{"echo": echo}
	for name, tc := range map[string]struct {
		extra   string
		wantSet bool
	}{
		"explicit false": {`,"providerExecuted":false`, true},
		"omitted":        {``, false},
	} {
		t.Run(name, func(t *testing.T) {
			mock := newMockHarness(mockHarnessOptions{
				script: func(submit func(string, interface{})) []StreamPart {
					stop := FinishReason{Unified: FinishReasonStop}
					return []StreamPart{
						&StreamStartPart{},
						mustDecodePart(t, `{"type":"tool-input-start","id":"c1","toolName":"echo"`+tc.extra+`}`),
						mustDecodePart(t, `{"type":"tool-input-end","id":"c1"}`),
						mustDecodePart(t, `{"type":"tool-call","toolCallId":"c1","toolName":"echo","input":"{}"`+tc.extra+`}`),
						&ToolResultPart{ToolCallID: "c1", ToolName: "echo", Result: "ok"},
						&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
						&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
					}
				},
			})
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: tools,
			})
			chunks := drainRunPrompt(t, out)
			starts := chunksOfType(chunks, provider.ChunkTypeToolInputStart)
			if len(starts) != 1 || starts[0].ToolCall.ProviderExecuted || starts[0].ToolCall.ProviderExecutedSet != tc.wantSet {
				t.Fatalf("tool-input-start = %+v, want providerExecuted false with set=%v", starts, tc.wantSet)
			}
			calls := chunksOfType(chunks, provider.ChunkTypeToolCall)
			if len(calls) != 1 || calls[0].ToolCall.ProviderExecuted || calls[0].ToolCall.ProviderExecutedSet != tc.wantSet {
				t.Fatalf("tool-call = %+v, want providerExecuted false with set=%v", calls, tc.wantSet)
			}
		})
	}
}

// TS enqueueApprovalResponse spreads `approval.providerExecuted` into the
// tool-approval-response part: true for a builtin approval.
func TestR43BuiltinApprovalResponseCarriesProviderExecuted(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				mustDecodePart(t, `{"type":"tool-call","toolCallId":"c1","toolName":"bash","input":"{}","providerExecuted":true,"dynamic":true}`),
				&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: false}},
	})
	chunks := drainRunPrompt(t, out)
	responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
	if len(responses) != 1 {
		t.Fatalf("approval responses = %+v, want 1", responses)
	}
	r := responses[0].ToolApprovalResponse
	if !r.ProviderExecuted {
		t.Fatalf("approval response providerExecuted = false, want true: %+v", r)
	}
	if !r.ToolCall.ProviderExecuted || !r.ToolCall.ProviderExecutedSet {
		t.Fatalf("approval response tool call = %+v, want providerExecuted true (set)", r.ToolCall)
	}
}

// A denied custom approval resumed from persisted state rebuilds its tool
// call from the approval record. TS copies `approval.providerExecuted`, so a
// record without it yields a tool call without providerExecuted, and an
// explicit false stays set.
func TestR43ApprovalContinuationToolCallProviderExecutedFromRecord(t *testing.T) {
	f := false
	for name, tc := range map[string]struct {
		providerExecuted *bool
		wantSet          bool
	}{
		"explicit false": {&f, true},
		"absent":         {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			mock := newMockHarness(mockHarnessOptions{})
			ask := types.Tool{Name: "ask", Parameters: map[string]interface{}{"type": "object"}}
			tools := map[string]types.Tool{"ask": ask}
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session, Mode: "continue", SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: tools,
				PendingToolApprovals: []PendingToolApproval{{
					ApprovalID: "a1", ToolCallID: "c1", ToolName: "ask", Input: `{}`,
					Kind: PendingToolApprovalCustom, ProviderExecuted: tc.providerExecuted,
				}},
				ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: false}},
			})
			chunks := drainRunPrompt(t, out)
			responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
			if len(responses) != 1 {
				t.Fatalf("approval responses = %+v, want 1", responses)
			}
			r := responses[0].ToolApprovalResponse
			if r.ProviderExecuted || r.ToolCall.ProviderExecuted || r.ToolCall.ProviderExecutedSet != tc.wantSet {
				t.Fatalf("approval response = %+v, want providerExecuted false with set=%v", r, tc.wantSet)
			}
		})
	}
}
