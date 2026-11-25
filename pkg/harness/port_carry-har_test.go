package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func portcarryharContextTool(executed *bool) types.Tool {
	return types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"userId": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"userId"},
		}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			*executed = true
			return nil, nil
		},
	}
}

// TS 1a94af38c7 "keeps host tool context out of the stream when an approved
// host tool fails context validation": the tool-error part carries the error
// the runtime received, the callback keeps the detailed one.
func TestPortcarryharApprovedContextValidationErrorStaysOutOfStream(t *testing.T) {
	executed := false
	tools := map[string]types.Tool{"lookup": portcarryharContextTool(&executed)}
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var endErr error
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue",
		Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
		ToolsContext: map[string]interface{}{"lookup": map[string]interface{}{"userId": 123, "apiKey": "host-secret"}},
		PendingToolApprovals: []PendingToolApproval{
			{ApprovalID: "a1", ToolCallID: "c1", ToolName: "lookup", Input: `{}`, Kind: PendingToolApprovalCustom},
		},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			endErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	chunks := drainRunPrompt(t, out)
	if executed {
		t.Fatal("tool executed despite invalid context")
	}
	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.Error == nil {
		t.Fatalf("tool-result chunks = %+v, want one tool-error", results)
	}
	if got := results[0].ToolResult.Error.Error(); got != "Tool context validation failed." {
		t.Fatalf("stream error = %q, want the text the runtime received", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if endErr == nil || !strings.Contains(endErr.Error(), "host-secret") {
		t.Fatalf("OnToolExecutionEnd error = %v, want the detailed host-side error", endErr)
	}
}

// A tool that panics with a value that is not an error or a string reaches
// the stream as the text the runtime received, never as the value (TS
// "sends the text that the runtime received when a host tool throws an
// object"). The turn pauses for a client tool, so the harness itself
// reports the host tool outcome.
func TestPortcarryharPanicValueStaysOutOfStream(t *testing.T) {
	tool := types.Tool{
		Name: "failing", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			panic(struct {
				Status int
				Body   string
			}{500, "host-secret"})
		},
	}
	askUser := types.Tool{Name: "askUser", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	tools := map[string]types.Tool{"failing": tool, "askUser": askUser}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "failing", Input: `{}`})
			emit(&ToolCallPart{ToolCallID: "c2", ToolName: "askUser", Input: `{}`})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, TotalUsage: stringUsage(1, 1)})
		},
		nil,
	)
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
	})
	chunks := drainRunPrompt(t, out)
	var failed *types.ToolResult
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolResult) {
		if c.ToolResult.ToolCallID == "c1" {
			failed = c.ToolResult
		}
	}
	if failed == nil || failed.Error == nil {
		t.Fatalf("no tool-error for c1 in %d chunks", len(chunks))
	}
	submitted, _ := mock.toolResults[0].Output.(map[string]interface{})
	if got := failed.Error.Error(); "Error: "+got != submitted["error"] {
		t.Fatalf("stream error %q differs from the submitted text %v", got, submitted["error"])
	}
}

// portcarryharEcho runs one host tool call whose echo the scripted runtime
// flags with echoIsError (nil: the submission's own flag), as in TS
// streamEchoedHostToolResult.
func portcarryharEcho(t *testing.T, tool types.Tool, echoIsError func(ToolResultSubmission) bool, approval ...ToolApprovalConfiguration) (toolOutcomes []provider.StreamChunk, endErr error) {
	t.Helper()
	tools := map[string]types.Tool{tool.Name: tool}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: tool.Name, Input: `{}`})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			isErr := sub.IsError
			if echoIsError != nil {
				isErr = echoIsError(sub)
			}
			emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: tool.Name, IsError: isErr, Result: sub.Output})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
	)
	var mu sync.Mutex
	var toolApproval ToolApprovalConfiguration
	if len(approval) > 0 {
		toolApproval = approval[0]
	}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
		SessionWorkDir: "/work/dir", ToolApproval: toolApproval,
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			endErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	chunks := drainRunPrompt(t, out)
	toolOutcomes = chunksOfType(chunks, provider.ChunkTypeToolResult)
	mu.Lock()
	defer mu.Unlock()
	return toolOutcomes, endErr
}

type portcarryharSecretErr struct{ APIKey string }

func (e *portcarryharSecretErr) Error() string { return "no forecast in /work/dir/forecast.json" }

// TS 7fffff9f02 'streams a failed host tool result as the runtime received
// it': one tool-error with the submitted output (work dir stripped), not
// provider-executed; the host callback keeps the thrown error.
func TestPortcarryharFailedHostToolEchoIsToolError(t *testing.T) {
	tool := types.Tool{
		Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, &portcarryharSecretErr{APIKey: "host-secret"}
		},
	}
	outcomes, endErr := portcarryharEcho(t, tool, nil)
	var secret *portcarryharSecretErr
	if !errors.As(endErr, &secret) {
		t.Fatalf("OnToolExecutionEnd error = %v, want the thrown error", endErr)
	}
	if len(outcomes) != 1 || outcomes[0].ToolResult.Error == nil {
		t.Fatalf("outcomes = %+v, want one tool-error", outcomes)
	}
	tr := outcomes[0].ToolResult
	if tr.ProviderExecuted {
		t.Fatal("a failed host tool error must not be provider-executed")
	}
	if got := tr.Error.Error(); got != `{"error":"Error: no forecast in forecast.json"}` {
		t.Fatalf("error text = %q", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", tr), "host-secret") {
		t.Fatalf("the secret reached the stream part: %+v", tr)
	}
}

// TS 7fffff9f02 test.each: whether a host tool failed is what the host
// submitted, not the echo's flag.
func TestPortcarryharEchoFlagFollowsSubmission(t *testing.T) {
	ok := types.Tool{
		Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"found": false}, nil
		},
		ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return &types.ToolResultOutput{Type: "error-text", Value: "Not found."}, nil
		},
	}
	failing := types.Tool{
		Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("lookup failed")
		},
	}
	// reason: flags a result whose model output is an error.
	outcomes, _ := portcarryharEcho(t, ok, func(ToolResultSubmission) bool { return true })
	if len(outcomes) != 1 || outcomes[0].ToolResult.Error != nil {
		t.Fatalf("flagged success = %+v, want one tool-result", outcomes)
	}
	// reason: flags a denied call.
	denied := types.Tool{
		Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"found": true}, nil
		},
	}
	outcomes, _ = portcarryharEcho(t, denied, func(ToolResultSubmission) bool { return true },
		ToolApprovalConfiguration{"lookup": ai.ToolApprovalStatusDenied})
	if len(outcomes) != 1 || outcomes[0].ToolResult.Error != nil {
		t.Fatalf("flagged denied call = %+v, want one tool-result", outcomes)
	}
	if m, _ := outcomes[0].ToolResult.Result.(map[string]interface{}); m["type"] != "execution-denied" {
		t.Fatalf("denied result = %#v, want execution-denied output", outcomes[0].ToolResult.Result)
	}
	// reason: flags that result inside submitToolResult is the same path here
	// (the scripted runtime echoes from the submit callback).
	// reason: drops the flag of a failed call.
	outcomes, _ = portcarryharEcho(t, failing, func(ToolResultSubmission) bool { return false })
	if len(outcomes) != 1 || outcomes[0].ToolResult.Error == nil {
		t.Fatalf("unflagged failure = %+v, want one tool-error", outcomes)
	}
	if got := outcomes[0].ToolResult.Error.Error(); got != `{"error":"Error: lookup failed"}` {
		t.Fatalf("error text = %q", got)
	}
}

// TS 'streams one tool-error and closes the stream when a host tool
// toModelOutput fails'.
func TestPortcarryharToModelOutputFailureIsOneToolError(t *testing.T) {
	tool := types.Tool{
		Name: "visualize", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
		ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return nil, errors.New("conversion failed")
		},
	}
	outcomes, _ := portcarryharEcho(t, tool, nil)
	if len(outcomes) != 1 || outcomes[0].ToolResult.Error == nil {
		t.Fatalf("outcomes = %+v, want one tool-error", outcomes)
	}
	if got := outcomes[0].ToolResult.Error.Error(); got != `{"error":"Error: conversion failed"}` {
		t.Fatalf("error text = %q", got)
	}
}

// TS 1a94af38c7 'keeps host tool context out of the stream when context
// validation fails' (pause path): the turn pauses for a client tool, so the
// harness itself reports the host tool outcome.
func TestPortcarryharContextValidationErrorStaysOutOfStreamOnPause(t *testing.T) {
	executed := false
	lookup := portcarryharContextTool(&executed)
	askUser := types.Tool{Name: "askUser", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	tools := map[string]types.Tool{"lookup": lookup, "askUser": askUser}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "lookup", Input: `{}`})
			emit(&ToolCallPart{ToolCallID: "c2", ToolName: "askUser", Input: `{}`})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, TotalUsage: stringUsage(1, 1)})
		},
		nil,
	)
	var mu sync.Mutex
	var hostErr error
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
		ToolsContext: map[string]interface{}{"lookup": map[string]interface{}{"userId": 123, "apiKey": "host-secret"}},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			hostErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	chunks := drainRunPrompt(t, out)
	if executed {
		t.Fatal("tool executed despite invalid context")
	}
	var failed *types.ToolResult
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolResult) {
		if c.ToolResult.ToolCallID == "c1" {
			failed = c.ToolResult
		}
	}
	if failed == nil || failed.Error == nil || failed.Error.Error() != "Tool context validation failed." {
		t.Fatalf("stream part = %+v, want a tool-error with the text the runtime received", failed)
	}
	if strings.Contains(fmt.Sprintf("%+v", chunks), "host-secret") {
		t.Fatal("the context value reached the stream")
	}
	mu.Lock()
	defer mu.Unlock()
	if hostErr == nil || !strings.Contains(hostErr.Error(), "host-secret") {
		t.Fatalf("OnToolExecutionEnd error = %v, want the detailed error", hostErr)
	}
}

// portcarryharPauseFailingTurn runs the turn of TS 'redacts a host tool error
// in %s when redactToolErrors is true': a failing host tool and a client tool
// in one step, so the turn pauses for the client tool and the harness
// reports the host tool outcome itself.
func portcarryharPauseFailingTurn() *runPromptOutput {
	failing := types.Tool{
		Name: "failing", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("host tool failed")
		},
	}
	askUser := types.Tool{Name: "askUser", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	tools := map[string]types.Tool{"failing": failing, "askUser": askUser}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "failing", Input: `{}`})
			emit(&ToolCallPart{ToolCallID: "c2", ToolName: "askUser", Input: `{}`})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, TotalUsage: stringUsage(1, 1)})
		}, nil)
	return runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
	})
}

// portcarryharAssertRedactedHostToolError checks the full stream (a tool-error
// part for c1 carrying the real error, not provider-executed) and the UI body.
func portcarryharAssertRedactedHostToolError(t *testing.T, out *runPromptOutput, uiBody func() string) {
	t.Helper()
	var parts []provider.StreamChunk
	stream := out.Result.Stream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	body := uiBody()
	<-out.Done
	var toolErrors []provider.StreamChunk
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolResult && p.ToolResult != nil && p.ToolResult.Error != nil {
			toolErrors = append(toolErrors, p)
		}
	}
	if len(toolErrors) != 1 || toolErrors[0].ToolResult.ToolCallID != "c1" ||
		!strings.Contains(toolErrors[0].ToolResult.Error.Error(), "host tool failed") || toolErrors[0].ToolResult.ProviderExecuted {
		t.Fatalf("tool-error parts = %+v, want one for c1 carrying the real error", toolErrors)
	}
	if !strings.Contains(body, `"errorText":"An error occurred."`) {
		t.Fatalf("UI body lacks the masked error text: %s", body)
	}
	if strings.Contains(body, "host tool failed") {
		t.Fatalf("UI body leaks the host tool error: %s", body)
	}
}

// TS: redacts a host tool error in toUIMessageStream when redactToolErrors is true.
func TestPortcarryharRedactToolErrorsHostToolErrorToUIMessageStream(t *testing.T) {
	out := portcarryharPauseFailingTurn()
	redact := true
	chunks, errs := out.Result.ToUIMessageStream(context.Background(), ai.UIMessageStreamResultOptions{RedactToolErrors: &redact}) //nolint:staticcheck // the harness result's UI entry point
	var body strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		var all []ai.UIMessageChunk
		for c := range chunks {
			all = append(all, c)
		}
		<-errs
		b, _ := json.Marshal(all)
		body.Write(b)
	}()
	portcarryharAssertRedactedHostToolError(t, out, func() string { <-done; return body.String() })
}

// TS: redacts a host tool error in toUIMessageStreamResponse when redactToolErrors is true.
func TestPortcarryharRedactToolErrorsHostToolErrorToUIMessageStreamResponse(t *testing.T) {
	out := portcarryharPauseFailingTurn()
	redact := true
	resp, err := out.Result.ToUIMessageStreamResponse(context.Background(), nil, ai.UIMessageStreamResultOptions{RedactToolErrors: &redact}) //nolint:staticcheck // the harness result's UI entry point
	if err != nil {
		t.Fatal(err)
	}
	bodyCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		bodyCh <- string(b)
	}()
	portcarryharAssertRedactedHostToolError(t, out, func() string { return <-bodyCh })
}

// TS: formats a failed host tool result with onError when tool errors are
// redacted. The runtime echoes the failed submission; OnError alone turns
// redaction on, so the one tool-output-error chunk carries the OnError text.
func TestPortcarryharFormatsFailedHostToolResultWithOnError(t *testing.T) {
	tool := types.Tool{
		Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("weather unavailable")
		},
	}
	tools := map[string]types.Tool{"weather": tool}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{}`})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "weather", IsError: sub.IsError, Result: sub.Output})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
	)
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
	})
	chunks, errs := out.Result.ToUIMessageStream(context.Background(), ai.UIMessageStreamResultOptions{OnError: func(error) string { return "Tool failed" }}) //nolint:staticcheck // the harness result's UI entry point
	var outputs []ai.UIMessageChunk
	for c := range chunks {
		if typ, _ := c["type"].(string); strings.HasPrefix(typ, "tool-output-") {
			outputs = append(outputs, c)
		}
	}
	if err := <-errs; err != nil {
		t.Fatalf("ui stream error: %v", err)
	}
	<-out.Done
	if len(outputs) != 1 || outputs[0]["type"] != "tool-output-error" || outputs[0]["toolCallId"] != "c1" || outputs[0]["errorText"] != "Tool failed" {
		t.Fatalf("tool-output chunks = %+v, want one tool-output-error c1 \"Tool failed\"", outputs)
	}
}
