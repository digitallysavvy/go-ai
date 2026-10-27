package fx

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports of the session-level "parallel fx approvals" and "stable fx
// continuation" cases of TS fx-session-native.test.ts, on the real core.

var sideEffect = regexp.MustCompile(`^printf (PARIS|TOKYO|MIXED)`)

func parallelContext(t *testing.T, kind string) (*libfxHarness, *gatewayServer, *fakeSandbox, *[]string, *sync.Mutex) {
	gw := &gatewayServer{t: t, fixtures: []string{"gateway-parallel-" + kind + "-0.sse.txt", "gateway-parallel-" + kind + "-1.sse.txt"}}
	h := newRealHarness(t, gw, Settings{})
	sb := newFakeSandbox()
	var mu sync.Mutex
	effects := &[]string{}
	bashEffects(sb, effects, &mu, sideEffect.MatchString, "")
	return h, gw, sb, effects, &mu
}

func toolCallIDs(parts []harness.StreamPart) []string {
	var ids []string
	for _, p := range parts {
		switch v := p.(type) {
		case *harness.ToolCallPart:
			ids = append(ids, v.ToolCallID)
		case *harness.ToolResultPart:
			ids = append(ids, v.ToolCallID)
		}
	}
	return ids
}

// TestLibfxRealCoreLaterCustomResultPersistsBeforeCallback mirrors TS
// "persists a later custom result before its callback and consumes it on
// cold continuation".
func TestLibfxRealCoreLaterCustomResultPersistsBeforeCallback(t *testing.T) {
	h, gw, sb, _, _ := parallelContext(t, "custom")
	ctx := context.Background()
	start := startOpts(sb, "parallel-custom")
	opts := harness.TurnSettings{Model: "spacexai/grok-4.7", Tools: []harness.ToolSpec{{Name: "weather", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}}}}}
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	waiting := newSignal()
	events := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Weather for Paris and Tokyo."), Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	calls := events.ofType(harness.PartTypeToolCall)
	if len(calls) != 2 || len(events.ofType(harness.PartTypeFinishStep)) != 1 {
		t.Fatalf("calls %d, finish-steps %d", len(calls), len(events.ofType(harness.PartTypeFinishStep)))
	}
	paris, tokyo := calls[0].(*harness.ToolCallPart).ToolCallID, calls[1].(*harness.ToolCallPart).ToolCallID
	err = control.SubmitToolResult(ctx, harness.ToolResultSubmission{
		ToolCallID: tokyo,
		Output:     map[string]any{"city": "Tokyo", "celsius": 18},
		ToolResult: &harness.ModelToolResult{Type: "tool-result", ToolCallID: tokyo, ToolName: "weather", Output: textOutput("TOKYO_MODEL_OUTPUT")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ids := toolCallIDs(events.ofType(harness.PartTypeToolResult)); len(ids) != 1 || ids[0] != tokyo {
		t.Fatalf("tool results = %v", ids)
	}
	_, stored := sb.stateFile(t)
	var st struct {
		Turn struct {
			Tools []struct {
				Status string `json:"status"`
				Result *struct {
					Text string `json:"text"`
				} `json:"result"`
			} `json:"tools"`
		} `json:"turn"`
	}
	if err := json.Unmarshal([]byte(stored), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Turn.Tools) != 2 || st.Turn.Tools[0].Status != "waiting" || st.Turn.Tools[0].Result != nil ||
		st.Turn.Tools[1].Status != "completed" || st.Turn.Tools[1].Result == nil || st.Turn.Tools[1].Result.Text != "TOKYO_MODEL_OUTPUT" {
		t.Fatalf("stored tools = %s", stored)
	}
	continueFrom, err := session.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	after := &partLog{}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := continued.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: paris, Output: map[string]any{"city": "Paris", "celsius": 12}}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued: %v", err)
	}
	if n := gw.postCount(); n != 2 {
		t.Errorf("posts = %d", n)
	}
	if !strings.Contains(gw.body(1), "TOKYO_MODEL_OUTPUT") {
		t.Error("second request lacks the model-facing Tokyo result")
	}
	if n := len(after.ofType(harness.PartTypeToolCall)) + len(after.ofType(harness.PartTypeToolApprovalRequest)); n != 0 {
		t.Errorf("continuation re-emitted %d calls/approvals", n)
	}
	if ids := toolCallIDs(after.ofType(harness.PartTypeToolResult)); len(ids) != 1 || ids[0] != paris {
		t.Errorf("continued results = %v", ids)
	}
}

// TestLibfxRealCoreBuiltinApprovalsAndLaterDenial mirrors TS "collects both
// built-in approvals and persists a later denial before cold continuation".
func TestLibfxRealCoreBuiltinApprovalsAndLaterDenial(t *testing.T) {
	h, gw, sb, effects, mu := parallelContext(t, "builtin")
	ctx := context.Background()
	start := startOpts(sb, "parallel-builtin")
	start.PermissionMode = harness.PermissionModeAllowEdits
	opts := harness.TurnSettings{Model: "spacexai/grok-4.7"}
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	waiting := newSignal()
	events := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Write Paris and Tokyo in parallel."), Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	var kinds []string
	var approvals []*harness.ToolApprovalRequestPart
	for _, p := range events.snapshot() {
		switch v := p.(type) {
		case *harness.ToolCallPart, *harness.FinishStepPart:
			kinds = append(kinds, p.PartType())
		case *harness.ToolApprovalRequestPart:
			kinds = append(kinds, p.PartType())
			approvals = append(approvals, v)
		}
	}
	if want := "tool-call,tool-approval-request,tool-call,tool-approval-request,finish-step"; strings.Join(kinds, ",") != want {
		t.Fatalf("parts = %v", kinds)
	}
	mu.Lock()
	if len(*effects) != 0 {
		t.Fatalf("effects before approval: %v", *effects)
	}
	mu.Unlock()
	if err := control.(harness.ToolApprovalSubmitter).SubmitToolApproval(ctx, harness.ToolApprovalSubmission{ApprovalID: approvals[1].ApprovalID, Approved: false, Reason: "Tokyo denied"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.allFiles(), "Tokyo denied") {
		t.Fatal("denial was not persisted")
	}
	continueFrom, err := session.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom, PermissionMode: start.PermissionMode})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	after := &partLog{}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := continued.(harness.ToolApprovalSubmitter).SubmitToolApproval(ctx, harness.ToolApprovalSubmission{ApprovalID: approvals[0].ApprovalID, Approved: true}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued: %v", err)
	}
	mu.Lock()
	if strings.Join(*effects, "|") != "printf PARIS > paris.txt" {
		t.Errorf("effects = %v", *effects)
	}
	mu.Unlock()
	if n := gw.postCount(); n != 2 || !strings.Contains(gw.body(1), "Tokyo denied") {
		t.Errorf("posts = %d, second carries denial: %v", n, strings.Contains(gw.body(1), "Tokyo denied"))
	}
	if n := len(after.ofType(harness.PartTypeToolCall)) + len(after.ofType(harness.PartTypeToolApprovalRequest)); n != 0 {
		t.Errorf("continuation re-emitted %d calls/approvals", n)
	}
	results := after.ofType(harness.PartTypeToolResult)
	if len(results) != 2 || results[0].(*harness.ToolResultPart).IsError || !results[1].(*harness.ToolResultPart).IsError {
		t.Errorf("continued results = %+v", results)
	}
}

// TestLibfxRealCoreLegacyStateFirstCallOnly mirrors TS "continues legacy
// state that only exposed the first call of a batch".
func TestLibfxRealCoreLegacyStateFirstCallOnly(t *testing.T) {
	h, gw, sb, _, _ := parallelContext(t, "custom")
	ctx := context.Background()
	start := startOpts(sb, "legacy-parallel")
	opts := harness.TurnSettings{Model: "spacexai/grok-4.7", Tools: []harness.ToolSpec{{Name: "weather"}}}
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	waiting := newSignal()
	events := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Weather for Paris and Tokyo."), Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	calls := toolCallIDs(events.ofType(harness.PartTypeToolCall))
	continueFrom, err := session.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	path, content := sb.stateFile(t)
	var stored map[string]any
	if err := json.Unmarshal([]byte(content), &stored); err != nil {
		t.Fatal(err)
	}
	turn := stored["turn"].(map[string]any)
	var tools []any
	for _, r := range turn["tools"].([]any) {
		if r.(map[string]any)["id"] == calls[0] {
			tools = append(tools, r)
		}
	}
	turn["tools"] = tools
	var keys []any
	for _, k := range turn["emittedKeys"].([]any) {
		switch k {
		case "call:" + calls[1]:
		case "waiting-step:0":
			keys = append(keys, "waiting:"+calls[0])
		default:
			keys = append(keys, k)
		}
	}
	turn["emittedKeys"] = keys
	data, _ := json.Marshal(stored)
	_ = sb.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: path, Content: string(data)})
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	second := newSignal()
	after := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			second.fire()
		}
	}}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := continued.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: calls[0], Output: "Paris"}); err != nil {
		t.Fatal(err)
	}
	second.wait(t)
	if ids := toolCallIDs(after.ofType(harness.PartTypeToolCall)); len(ids) != 1 || ids[0] != calls[1] {
		t.Fatalf("continued calls = %v", ids)
	}
	if err := continued.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: calls[1], Output: "Tokyo"}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued: %v", err)
	}
	if n := gw.postCount(); n != 2 {
		t.Errorf("posts = %d", n)
	}
}

// TestLibfxRealCoreParsedOutputColdReplay mirrors TS "maps parsed gateway
// output and usage without repeating text on cold replay".
func TestLibfxRealCoreParsedOutputColdReplay(t *testing.T) {
	gw := &gatewayServer{t: t, fixtures: []string{"gateway-1.sse.txt"}}
	h := newRealHarness(t, gw, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	start := startOpts(sb, "parsed-gateway")
	start.BuiltinToolFiltering = &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow}
	opts := harness.TurnSettings{Model: "anthropic/claude-haiku-4-5"}
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	suspended := make(chan (<-chan *harness.ContinueTurnState), 1)
	var once sync.Once
	before := &partLog{}
	before.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeTextDelta {
			once.Do(func() { suspended <- suspendFromEmit(t, session) })
		}
	}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Reply."), Emit: before.emit})
	if err != nil {
		t.Fatal(err)
	}
	continueFrom := <-<-suspended
	_ = waitDone(t, control)
	if continueFrom == nil {
		t.Fatal("no continuation")
	}
	requests := gw.postCount()
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, BuiltinToolFiltering: start.BuiltinToolFiltering, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	after := &partLog{}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued: %v", err)
	}
	if n := gw.postCount(); n != requests {
		t.Errorf("cold replay fetched: %d -> %d posts", requests, n)
	}
	all := &partLog{parts: append(before.snapshot(), after.snapshot()...)}
	if got := all.text(); len(got) != 5 {
		t.Errorf("text = %q, want 5 characters", got)
	}
	if n := len(all.ofType(harness.PartTypeTextStart)); n != 1 {
		t.Errorf("text-start parts = %d", n)
	}
	if n := len(all.ofType(harness.PartTypeTextEnd)); n != 1 {
		t.Errorf("text-end parts = %d", n)
	}
	if len(after.ofType(harness.PartTypeError)) != 0 {
		t.Errorf("errors: %+v", after.ofType(harness.PartTypeError))
	}
	finishes := after.ofType(harness.PartTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("finish parts = %d", len(finishes))
	}
	f := finishes[0].(*harness.FinishPart)
	if f.FinishReason != (harness.FinishReason{Unified: "stop", Raw: "stop"}) {
		t.Errorf("finish reason = %+v", f.FinishReason)
	}
	if f.TotalUsage.InputTokens.Total == nil || *f.TotalUsage.InputTokens.Total <= 0 {
		t.Errorf("total usage = %+v", f.TotalUsage)
	}
}
