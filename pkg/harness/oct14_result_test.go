package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS #23086: interleaved text/reasoning blocks accumulate by stream ID
// into separate content parts with their own metadata.
func TestRunPrompt_InterleavedBlocksPreservedByStreamID(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "text-a", HarnessMetadata: Metadata{"mock": {"block": "text-a"}}},
				&TextDeltaPart{ID: "text-a", Delta: "A1"},
				&TextStartPart{ID: "text-b", HarnessMetadata: Metadata{"mock": {"block": "text-b"}}},
				&TextDeltaPart{ID: "text-b", Delta: "B1"},
				&TextDeltaPart{ID: "text-a", Delta: "A2"},
				&TextEndPart{ID: "text-a"}, &TextEndPart{ID: "text-b"},
				&ReasoningStartPart{ID: "reasoning-a", HarnessMetadata: Metadata{"mock": {"block": "reasoning-a"}}},
				&ReasoningDeltaPart{ID: "reasoning-a", Delta: "R1"},
				&ReasoningStartPart{ID: "reasoning-b", HarnessMetadata: Metadata{"mock": {"block": "reasoning-b"}}},
				&ReasoningDeltaPart{ID: "reasoning-b", Delta: "S1"},
				&ReasoningDeltaPart{ID: "reasoning-a", Delta: "R2"},
				&ReasoningEndPart{ID: "reasoning-a"}, &ReasoningEndPart{ID: "reasoning-b"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	drainRunPrompt(t, out)

	content := out.Result.FinalStep().Content
	type want struct {
		kind, text, meta string
	}
	wants := []want{
		{"text", "A1A2", `{"mock":{"block":"text-a"}}`},
		{"text", "B1", `{"mock":{"block":"text-b"}}`},
		{"reasoning", "R1R2", `{"mock":{"block":"reasoning-a"}}`},
		{"reasoning", "S1", `{"mock":{"block":"reasoning-b"}}`},
	}
	if len(content) != len(wants) {
		t.Fatalf("content = %+v, want %d parts", content, len(wants))
	}
	for i, w := range wants {
		var text, meta string
		switch p := content[i].(type) {
		case types.TextContent:
			if w.kind != "text" {
				t.Fatalf("part %d is text, want %s", i, w.kind)
			}
			text, meta = p.Text, string(p.ProviderMetadata)
		case types.ReasoningContent:
			if w.kind != "reasoning" {
				t.Fatalf("part %d is reasoning, want %s", i, w.kind)
			}
			text, meta = p.Text, string(p.ProviderMetadata)
		default:
			t.Fatalf("part %d unexpected type %T", i, content[i])
		}
		if text != w.text || meta != w.meta {
			t.Fatalf("part %d = %q %s, want %q %s", i, text, meta, w.text, w.meta)
		}
	}
	// Text() concatenates blocks in arrival order of the deltas.
	if got := out.Result.FinalStep().Text; got != "A1B1A2" {
		t.Fatalf("step text = %q", got)
	}
}

// Ports TS #23085: response messages are built per step and concatenated, so
// a tool-call step followed by an answer step is assistant/tool/assistant.
func TestRunPrompt_ResponseMessagesPreserveChronology(t *testing.T) {
	lookup := types.Tool{
		Name: "lookup", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return 42, nil
		},
	}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "lookup-call", ToolName: "lookup", Input: "{}"})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: "lookup-call", ToolName: "lookup", Result: 42})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&TextDeltaPart{ID: "answer", Delta: "The answer is 42."})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)})
		},
	)
	tools := map[string]types.Tool{"lookup": lookup}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("Look it up"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)

	if n := len(out.Result.Steps()); n != 2 {
		t.Fatalf("steps = %d, want 2", n)
	}
	msgs := out.Result.ResponseMessages()
	var roles []types.MessageRole
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	want := []types.MessageRole{types.RoleAssistant, types.RoleTool, types.RoleAssistant}
	if len(roles) != 3 || roles[0] != want[0] || roles[1] != want[1] || roles[2] != want[2] {
		t.Fatalf("roles = %v, want assistant/tool/assistant", roles)
	}
	last, ok := msgs[2].Content[0].(types.TextContent)
	if !ok || last.Text != "The answer is 42." {
		t.Fatalf("final message = %+v", msgs[2])
	}
	steps := out.Result.Steps()
	if len(steps[0].ResponseMessages) != 2 || len(steps[1].ResponseMessages) != 1 {
		t.Fatalf("per-step response messages = %d/%d, want 2/1", len(steps[0].ResponseMessages), len(steps[1].ResponseMessages))
	}
}

// Ports TS #23096: successful (and failed) tool results restore the parsed
// input and execution origin from their originating tool call, and the
// provider-executed result lands on the assistant message while the host
// result lands on the tool message.
func TestRunPrompt_ToolResultsPreserveInputAndOrigin(t *testing.T) {
	host := types.Tool{
		Name: "hostLookup", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "host found", nil
		},
	}
	allCalls := make(chan struct{})
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			defer close(allCalls)
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "native-success", ToolName: "nativeLookup", Input: `{"query":"native success"}`, ProviderExecuted: true, Dynamic: true})
			emit(&ToolCallPart{ToolCallID: "host-success", ToolName: "hostLookup", Input: `{"query":"host success"}`})
			emit(&ToolCallPart{ToolCallID: "native-failure", ToolName: "nativeLookup", Input: `{"query":"native failure"}`, ProviderExecuted: true, Dynamic: true})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			<-allCalls
			emit(&ToolResultPart{ToolCallID: "host-success", ToolName: "hostLookup", Result: "host found"})
			emit(&ToolResultPart{ToolCallID: "native-failure", ToolName: "nativeLookup", Result: "native failed", IsError: true, Dynamic: true})
			emit(&ToolResultPart{ToolCallID: "native-success", ToolName: "nativeLookup", Result: "native found", Dynamic: true})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
	)
	tools := map[string]types.Tool{"hostLookup": host}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	chunks := drainRunPrompt(t, out)

	byID := map[string]*types.ToolResult{}
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolResult) {
		byID[c.ToolResult.ToolCallID] = c.ToolResult
	}
	ns, hs, nf := byID["native-success"], byID["host-success"], byID["native-failure"]
	if ns == nil || hs == nil || nf == nil {
		t.Fatalf("results = %+v", byID)
	}
	if ns.Input["query"] != "native success" || !ns.ProviderExecuted || ns.Result != "native found" {
		t.Fatalf("native success = %+v", ns)
	}
	if hs.Input["query"] != "host success" || hs.ProviderExecuted {
		t.Fatalf("host success = %+v", hs)
	}
	if nf.Input["query"] != "native failure" || !nf.ProviderExecuted || nf.Error == nil {
		t.Fatalf("native failure = %+v", nf)
	}

	var assistant, toolMsg *types.Message
	for _, m := range out.Result.ResponseMessages() {
		m := m
		switch m.Role {
		case types.RoleAssistant:
			assistant = &m
		case types.RoleTool:
			toolMsg = &m
		}
	}
	hasResult := func(m *types.Message, id string) bool {
		if m == nil {
			return false
		}
		for _, p := range m.Content {
			if r, ok := p.(types.ToolResultContent); ok && r.ToolCallID == id {
				return true
			}
		}
		return false
	}
	if !hasResult(assistant, "native-success") {
		t.Fatalf("assistant message should carry the provider-executed result: %+v", assistant)
	}
	if !hasResult(toolMsg, "host-success") || hasResult(toolMsg, "native-success") {
		t.Fatalf("tool message = %+v, want host result only", toolMsg)
	}
}

func TestTranslatePart_ToolResultRestoresInputAndOrigin(t *testing.T) {
	opts := TranslateOptions{GetToolCall: func(string) (ToolCallInfo, bool) {
		return ToolCallInfo{Input: map[string]interface{}{"city": "San Francisco"}, ProviderExecuted: true}, true
	}}
	ok := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "weather", Result: "sunny"}, opts)[0].ToolResult
	if ok.Input["city"] != "San Francisco" || !ok.ProviderExecuted || ok.Error != nil {
		t.Fatalf("success = %+v", ok)
	}
	bad := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "weather", Result: "upstream timeout", IsError: true}, opts)[0].ToolResult
	if bad.Input["city"] != "San Francisco" || !bad.ProviderExecuted || bad.Error == nil {
		t.Fatalf("failure = %+v", bad)
	}
}
