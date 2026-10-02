package harness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Ports TS run-prompt.test.ts's "runPrompt host tool input validation" and
// "runPrompt workDir stripping" describe blocks (validate-tool-call.test.ts's
// coverage of the underlying validator lives at the pkg/ai layer already,
// via TestParseToolCall — these tests are about run_prompt.go's *wiring* of
// that validator: raw-vs-display validation, execution-time argument
// selection, revalidation on approval continuations, and the invalid
// short-circuit). Go's pkg/schema JSON Schema validator supports a narrower
// keyword set than TS's zod (no string length/pattern/array-length
// constraints), so the schemas below use `type`/`enum`/`required` instead of
// TS's `.min()`/`.max()`/`.startsWith()` — same coverage, different knobs.

// drainRunPrompt reads every chunk off a runPrompt call's stream and waits
// for the driver goroutine to finish (mirrors TS's `for await (const part of
// result.fullStream) parts.push(part); await done;`).
func drainRunPrompt(t *testing.T, out *runPromptOutput) []provider.StreamChunk {
	t.Helper()
	var chunks []provider.StreamChunk
	stream := out.Result.Stream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, *c)
	}
	<-out.Done
	return chunks
}

func chunksOfType(chunks []provider.StreamChunk, want provider.ChunkType) []provider.StreamChunk {
	var out []provider.StreamChunk
	for _, c := range chunks {
		if c.Type == want {
			out = append(out, c)
		}
	}
	return out
}

// restrictedSchema is a JSON Schema a host tool's Parameters can use across
// these tests: requires "city" (string) and "mode" (must be "read").
func restrictedSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"city", "mode"},
		"properties": map[string]interface{}{
			"city": map[string]interface{}{"type": "string"},
			"mode": map[string]interface{}{"type": "string", "enum": []interface{}{"read"}},
		},
	}
}

// TestRunPrompt_HostToolInputValidation_RejectsBeforeExecutingOrApproving
// ports TS's `test.each([...])('rejects %s before executing or requesting
// approval', ...)`: a host tool call whose input fails schema validation (or
// isn't even valid JSON) must never reach tool.Execute, and must never
// surface as a tool-approval-request even when the harness itself also emits
// one for the same toolCallId (the settled-replay suppression must apply to
// it too) — it settles immediately as a tool-error, with the generic
// invalidToolInputMessage on both the consumer stream and the harness
// submission.
func TestRunPrompt_HostToolInputValidation_RejectsBeforeExecutingOrApproving(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"wrong primitive type", `{"city":1,"mode":"read"}`},
		{"rejected enum", `{"city":"Paris","mode":"write"}`},
		{"missing required field", `{"city":"Paris"}`},
		{"malformed JSON", `{"city":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			restricted := types.Tool{
				Name:       "restricted",
				Parameters: restrictedSchema(),
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executed = true
					return "not reached", nil
				},
			}
			mock := newMockHarness(mockHarnessOptions{
				script: func(submit func(string, interface{})) []StreamPart {
					return []StreamPart{
						&StreamStartPart{},
						&ToolCallPart{ToolCallID: "c1", ToolName: "restricted", Input: tt.input},
						// The harness may still emit its own approval request
						// for the same call; it must be suppressed too (the
						// call already settled as invalid).
						&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
						&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
						&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
					}
				},
			})
			tools := map[string]types.Tool{"restricted": restricted}
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session,
				Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
				SandboxSession: testSandbox(),
				ToolApproval:   ToolApprovalConfiguration{"restricted": ai.ToolApprovalStatusUserApproval},
			})
			chunks := drainRunPrompt(t, out)

			if executed {
				t.Fatal("tool must not execute: invalid input")
			}
			if got := chunksOfType(chunks, provider.ChunkTypeToolApprovalRequest); len(got) != 0 {
				t.Fatalf("tool-approval-request chunks = %+v, want none", got)
			}
			toolResults := chunksOfType(chunks, provider.ChunkTypeToolResult)
			if len(toolResults) != 1 {
				t.Fatalf("tool-result chunks = %+v, want exactly one tool-error", toolResults)
			}
			tr := toolResults[0].ToolResult
			if tr == nil || tr.Error == nil || tr.Error.Error() != invalidToolInputMessage {
				t.Fatalf("tool-result = %+v, want an error %q", tr, invalidToolInputMessage)
			}
			if !tr.Dynamic {
				t.Fatalf("tool-result.Dynamic = false, want true (TS dynamic:true on the invalid tool-error part)")
			}

			if len(mock.toolResults) != 1 {
				t.Fatalf("submitted results = %+v, want exactly one", mock.toolResults)
			}
			sub := mock.toolResults[0]
			if sub.ToolCallID != "c1" || !sub.IsError {
				t.Fatalf("submitted = %+v", sub)
			}
			outMap, ok := sub.Output.(map[string]interface{})
			if !ok || outMap["error"] != invalidToolInputMessage {
				t.Fatalf("submitted.Output = %+v, want {error: %q}", sub.Output, invalidToolInputMessage)
			}
		})
	}
}

// TestRunPrompt_HostTool_ContextSchemaAppliesDefaultsBeforeValidating covers
// SC2 item 2: executeHostToolAsync used to validate a host tool's
// ToolsContext entry against ContextSchema before any schema defaults were
// applied, so a required context field with a schema default -- but absent
// from ToolsContext -- would incorrectly fail with "Tool context validation
// failed." even though the tool never needed the caller to supply it. This
// mirrors pkg/agent/toolloop.go's validateAgentToolContext and
// pkg/ai/tool_approval.go's identical apply-then-validate order.
func TestRunPrompt_HostTool_ContextSchemaAppliesDefaultsBeforeValidating(t *testing.T) {
	contextSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"userId": map[string]interface{}{"type": "string"},
			"locale": map[string]interface{}{"type": "string", "default": "en-US"},
		},
		"required": []interface{}{"userId", "locale"},
	})

	var mu sync.Mutex
	var executedToolContext interface{}
	tool := types.Tool{
		Name: "greet",
		Parameters: map[string]interface{}{
			"type":       "object",
			"required":   []interface{}{"name"},
			"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		},
		ContextSchema: contextSchema,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			executedToolContext = opts.ToolContext
			mu.Unlock()
			return map[string]interface{}{"ok": true}, nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "greet", Input: `{"name":"world"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&ToolResultPart{ToolCallID: "c1", ToolName: "greet", Result: map[string]interface{}{"ok": true}},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	tools := map[string]types.Tool{"greet": tool}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
		// "locale" is absent -- it only exists as a schema default.
		ToolsContext: map[string]interface{}{"greet": map[string]interface{}{"userId": "u1"}},
	})
	chunks := drainRunPrompt(t, out)

	if got := chunksOfType(chunks, provider.ChunkTypeToolResult); len(got) != 1 || got[0].ToolResult == nil || got[0].ToolResult.Error != nil {
		t.Fatalf("tool-result chunks = %+v, want exactly one success (no context validation error)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	ctxMap, ok := executedToolContext.(map[string]interface{})
	if !ok || ctxMap["userId"] != "u1" || ctxMap["locale"] != "en-US" {
		t.Fatalf("executed tool context = %#v, want the default locale filled in", executedToolContext)
	}
}

// TestRunPrompt_HostTool_ExecutesWithAbsolutePath_DisplayIsStripped ports
// TS's "strips the workDir for consumers but executes host tools with the
// absolute path": a host tool must execute with the schema-validated but
// *unstripped* input (the real absolute path, which the sandbox needs to
// resolve the file), while the tool-call the consumer stream sees has the
// work-dir prefix stripped to a workspace-relative path.
func TestRunPrompt_HostTool_ExecutesWithAbsolutePath_DisplayIsStripped(t *testing.T) {
	const workDir = "/vercel/sandbox/claude-code-abc123"
	var executedArgs map[string]interface{}
	readFile := types.Tool{
		Name: "readFile",
		Parameters: map[string]interface{}{
			"type":       "object",
			"required":   []interface{}{"path"},
			"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executedArgs = input
			return map[string]interface{}{"ok": true}, nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "readFile", Input: `{"path":"` + workDir + `/src/foo.ts"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// The adapter echoes the host's submitted result back.
				&ToolResultPart{ToolCallID: "c1", ToolName: "readFile", Result: workDir + "/src/foo.ts"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	tools := map[string]types.Tool{"readFile": readFile}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), SessionWorkDir: workDir,
	})
	chunks := drainRunPrompt(t, out)

	// Host tool executes with the original absolute path so it resolves
	// against the sandbox root.
	if executedArgs == nil || executedArgs["path"] != workDir+"/src/foo.ts" {
		t.Fatalf("executedArgs = %+v, want {path: %q}", executedArgs, workDir+"/src/foo.ts")
	}

	// The consumer-facing tool-call has a workspace-relative path.
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall == nil {
		t.Fatalf("tool-call chunks = %+v, want exactly one", toolCalls)
	}
	if got := toolCalls[0].ToolCall.Arguments["path"]; got != "src/foo.ts" {
		t.Fatalf("displayed path = %v, want workspace-relative %q", got, "src/foo.ts")
	}

	// The consumer-facing tool-result is stripped too.
	toolResults := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(toolResults) != 1 || toolResults[0].ToolResult == nil {
		t.Fatalf("tool-result chunks = %+v, want exactly one", toolResults)
	}
	if got := toolResults[0].ToolResult.Result; got != "src/foo.ts" {
		t.Fatalf("displayed result = %v, want workspace-relative %q", got, "src/foo.ts")
	}
}

// TestRunPrompt_HostTool_RejectsRawPathValidOnlyAfterStripping ports TS's
// "rejects raw paths that only satisfy the schema after display stripping" —
// the regression test for the exact bug the audit found: StripWorkDir must
// never run before validation/execution. The schema below accepts only the
// workspace-relative form ("src/foo.ts"); if the harness validated (or
// executed with) the display-stripped value instead of the raw one, this
// tool call would incorrectly be treated as valid and executed.
func TestRunPrompt_HostTool_RejectsRawPathValidOnlyAfterStripping(t *testing.T) {
	const workDir = "/vercel/sandbox/claude-code-abc123"
	executed := false
	readFile := types.Tool{
		Name: "readFile",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"path"},
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "enum": []interface{}{"src/foo.ts"}},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "not reached", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "readFile", Input: `{"path":"` + workDir + `/src/foo.ts"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"readFile": readFile}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), SessionWorkDir: workDir,
	})
	chunks := drainRunPrompt(t, out)

	if executed {
		t.Fatal("tool must not execute: the raw (unstripped) path fails the schema")
	}
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall == nil {
		t.Fatalf("tool-call chunks = %+v, want exactly one", toolCalls)
	}
	tc := toolCalls[0].ToolCall
	if !tc.Invalid {
		t.Fatalf("tool-call.Invalid = false, want true")
	}
	if got := tc.Arguments["path"]; got != "src/foo.ts" {
		t.Fatalf("displayed (invalid) path = %v, want stripped %q", got, "src/foo.ts")
	}
	if tc.Error == nil || tc.Error.Error() != invalidToolInputMessage {
		t.Fatalf("tool-call.Error = %v, want %q", tc.Error, invalidToolInputMessage)
	}

	if len(mock.toolResults) != 1 || !mock.toolResults[0].IsError {
		t.Fatalf("submitted results = %+v, want exactly one error result", mock.toolResults)
	}
	outMap, ok := mock.toolResults[0].Output.(map[string]interface{})
	if !ok || outMap["error"] != invalidToolInputMessage {
		t.Fatalf("submitted.Output = %+v, want {error: %q}", mock.toolResults[0].Output, invalidToolInputMessage)
	}
}

// TestRunPrompt_HostTool_RevalidatesApprovedContinuationInput ports TS's
// `test.each([['valid',...,true],['invalid',...,false]])('revalidates %s
// approved continuation input', ...)`: a custom tool-approval request's
// input is only ever JSON-parsed (never schema-validated) when the pending
// approval is first recorded. Once the caller approves it — here via a
// startup continuation resuming a suspended turn, so there is no
// in-process validation history to reuse even by accident — run_prompt.go
// must revalidate against the schema before executing, and reject it the
// same way a first-pass invalid call is rejected when it fails.
func TestRunPrompt_HostTool_RevalidatesApprovedContinuationInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"valid", `{"city":"Paris"}`, true},
		{"invalid", `{"city":42}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			var executedArgs map[string]interface{}
			weather := types.Tool{
				Name: "weather",
				Parameters: map[string]interface{}{
					"type":       "object",
					"required":   []interface{}{"city"},
					"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
				},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executed = true
					executedArgs = input
					return input, nil
				},
			}
			mock := newMockHarness(mockHarnessOptions{
				continueScript: func(submit func(string, interface{})) []StreamPart {
					// A FinishStepPart is required to close the step the
					// resumed approval reopened (see consumeLoop's
					// closingResumedStep handling) before the terminal
					// FinishPart.
					return []StreamPart{
						&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
						&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
					}
				},
			})
			tools := map[string]types.Tool{"weather": weather}
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session,
				Mode: "continue", Tools: tools, ActiveTools: tools,
				SandboxSession: testSandbox(),
				PendingToolApprovals: []PendingToolApproval{
					{ApprovalID: "a1", ToolCallID: "c1", ToolName: "weather", Input: tt.input, Kind: PendingToolApprovalCustom},
				},
				ToolApprovalContinuations: []types.ToolApprovalResponseContent{
					{ApprovalID: "a1", ToolCallID: "c1", Approved: true},
				},
			})
			chunks := drainRunPrompt(t, out)

			if executed != tt.valid {
				t.Fatalf("executed = %v, want %v", executed, tt.valid)
			}
			if len(mock.toolResults) != 1 {
				t.Fatalf("submitted results = %+v, want exactly one", mock.toolResults)
			}
			sub := mock.toolResults[0]
			if sub.ToolCallID != "c1" {
				t.Fatalf("submitted.ToolCallID = %q, want c1", sub.ToolCallID)
			}
			if tt.valid {
				if sub.IsError {
					t.Fatalf("submitted = %+v, want a success result", sub)
				}
				if executedArgs["city"] != "Paris" {
					t.Fatalf("executedArgs = %+v", executedArgs)
				}
				// Whether a successful approval-continuation execution's
				// result also reaches the consumer stream as its own
				// ChunkTypeToolResult (versus only the harness submission
				// asserted above) is a pre-existing, separate question from
				// this slice's schema-revalidation gap and isn't asserted
				// here.
			} else {
				if !sub.IsError {
					t.Fatalf("submitted = %+v, want an error result", sub)
				}
				outMap, ok := sub.Output.(map[string]interface{})
				if !ok || outMap["error"] != invalidToolInputMessage {
					t.Fatalf("submitted.Output = %+v, want {error: %q}", sub.Output, invalidToolInputMessage)
				}
				toolResults := chunksOfType(chunks, provider.ChunkTypeToolResult)
				if len(toolResults) != 1 || toolResults[0].ToolResult.Error == nil {
					t.Fatalf("tool-result chunks = %+v, want one tool-error", toolResults)
				}
			}
		})
	}
}

// TestRunPrompt_CtxCancellation_JoinsOutstandingHostToolExecutions is the
// regression test for bug-review/R3.md's "Unverified" consumeLoop gap:
// consumeLoop's `case <-d.ctx.Done()` branch used to return immediately,
// without calling joinOutstandingExecutions first — unlike every other exit
// path from the read loop (an ErrorPart, an invalid host tool call, a
// finish-step, or the terminal finish). That left a host tool's Execute
// goroutine (started by executeHostToolAsync) still running after the turn
// had already been reported finished/failed to the caller: a goroutine
// leak, and a window in which that goroutine's eventual SubmitToolResult
// call lands on a turn (or, via AgentSession, a whole new turn on the same
// session) that has already moved on. Mirrors TS run-prompt.ts, where every
// exit from its read loop — including one driven by the caller's own
// abortSignal — runs `await waitForOutstandingHostToolExecutions()` first.
//
// This drives a host tool whose Execute blocks until the test releases it,
// cancels the turn's ctx while that execution is still outstanding (with
// nothing else queued, so consumeLoop's read select is genuinely parked on
// both the partsCh and ctx.Done() branches — promptDone keeps the mock's
// PromptControl "running" so partsCh is never closed out from under the
// race), and asserts runPrompt's Done signal does not fire until the tool
// execution actually completes.
func TestRunPrompt_CtxCancellation_JoinsOutstandingHostToolExecutions(t *testing.T) {
	toolStarted := make(chan struct{})
	release := make(chan struct{})
	slow := types.Tool{
		Name: "slow",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			close(toolStarted)
			<-release
			return map[string]interface{}{"ok": true}, nil
		},
	}

	// keepAlive holds the mock's PromptControl.Done() open past the point
	// the script runs out of queued parts, so run_prompt's own
	// `<-control.Done()` goroutine never closes partsCh during this test —
	// otherwise that closure could race the ctx cancellation below for which
	// branch of consumeLoop's select fires.
	keepAlive := make(chan struct{})
	t.Cleanup(func() { close(keepAlive) })

	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "slow", Input: `{}`},
			}
		},
		promptDone: func() <-chan struct{} { return keepAlive },
	})
	tools := map[string]types.Tool{"slow": slow}
	ctx, cancel := context.WithCancel(context.Background())
	out := runPrompt(ctx, runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
	})

	select {
	case <-toolStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("host tool execution never started")
	}

	cancel()

	// With the fix, consumeLoop must block in joinOutstandingExecutions
	// until the "slow" tool actually finishes, so Done must not fire yet.
	select {
	case <-out.Done:
		t.Fatal("runPrompt settled before the outstanding host tool execution finished: ctx cancellation did not join it")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case <-out.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("runPrompt never settled after the outstanding host tool execution finished")
	}

	if err := out.Result.Err(); err == nil {
		t.Fatal("Result.Err() = nil, want the ctx cancellation error")
	}
}

// concurrentToolsSession is a minimal Session for
// TestRunPrompt_PublishesConcurrentHostToolLifecycleAndResultsPerTool,
// mirroring TS run-prompt.test.ts's inline mock session exactly: it emits
// only stream-start + the two tool-call events upfront. SubmitToolResult
// (called concurrently from each host tool's own exec goroutine) echoes
// that tool's result back as a tool-result StreamPart immediately, and
// only emits the step/turn-finishing parts once BOTH tools have reported —
// finish-step never arrives mid-execution, so it can never race
// run_prompt.go's own joinOutstandingExecutions barrier the way emitting it
// right after the tool-call events would.
type concurrentToolsSession struct {
	toolNames map[string]string

	mu        sync.Mutex
	completed map[string]bool
	emit      EmitFunc
	done      chan struct{}
}

func (s *concurrentToolsSession) SessionID() string { return "concurrent-tools" }
func (s *concurrentToolsSession) IsResume() bool    { return false }

func (s *concurrentToolsSession) DoPromptTurn(_ context.Context, opts PromptTurnOptions) (PromptControl, error) {
	s.mu.Lock()
	s.completed = map[string]bool{}
	s.emit = opts.Emit
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		opts.Emit(&StreamStartPart{})
		opts.Emit(&ToolCallPart{ToolCallID: "c-fast", ToolName: "fast", Input: "{}", StepToolCallCount: intPtr(2)})
		opts.Emit(&ToolCallPart{ToolCallID: "c-slow", ToolName: "slow", Input: "{}", StepToolCallCount: intPtr(2)})
	}()
	return &concurrentToolsControl{session: s, done: done}, nil
}

func (s *concurrentToolsSession) DoContinueTurn(context.Context, ContinueTurnOptions) (PromptControl, error) {
	return nil, nil
}
func (s *concurrentToolsSession) DoCompact(context.Context, string) error { return nil }
func (s *concurrentToolsSession) DoSuspendTurn(context.Context) (*ContinueTurnState, error) {
	return nil, nil
}
func (s *concurrentToolsSession) DoDetach(context.Context) (*ResumeSessionState, error) {
	return nil, nil
}
func (s *concurrentToolsSession) DoStop(context.Context) (*ResumeSessionState, error) {
	return nil, nil
}
func (s *concurrentToolsSession) DoDestroy(context.Context) error { return nil }

func intPtr(n int) *int { return &n }

type concurrentToolsControl struct {
	session *concurrentToolsSession
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

func (c *concurrentToolsControl) SubmitToolResult(_ context.Context, r ToolResultSubmission) error {
	s := c.session
	s.emit(&ToolResultPart{ToolCallID: r.ToolCallID, ToolName: s.toolNames[r.ToolCallID], Result: r.Output, IsError: r.IsError})

	s.mu.Lock()
	s.completed[r.ToolCallID] = true
	allDone := len(s.completed) == 2
	s.mu.Unlock()
	if allDone {
		s.emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(2, 2)})
		s.emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)})
		close(c.done)
	}
	return nil
}
func (c *concurrentToolsControl) Done() <-chan struct{} { return c.done }
func (c *concurrentToolsControl) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// TestRunPrompt_PublishesConcurrentHostToolLifecycleAndResultsPerTool ports
// TS "publishes concurrent host tool lifecycle events and results as each
// tool runs" (run-prompt.test.ts, TS #21696 "Start/end callbacks and fast
// tool results were delayed behind slower tools in the same step"): two
// host tools in the same step run concurrently; the fast one's
// OnToolExecutionEnd and streamed tool-result must not wait for the slow
// one to finish, and both tools' OnToolExecutionStart must fire while
// execution is still in progress, not only once an outcome is known.
func TestRunPrompt_PublishesConcurrentHostToolLifecycleAndResultsPerTool(t *testing.T) {
	releaseSlow := make(chan struct{})
	slowStarted := make(chan struct{})
	fast := types.Tool{
		Name:       "fast",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "fast-done", nil
		},
	}
	slow := types.Tool{
		Name:       "slow",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			close(slowStarted)
			select {
			case <-releaseSlow:
			case <-ctx.Done():
			}
			return "slow-done", nil
		},
	}
	tools := map[string]types.Tool{"fast": fast, "slow": slow}
	session := &concurrentToolsSession{toolNames: map[string]string{"c-fast": "fast", "c-slow": "slow"}}

	var cbMu sync.Mutex
	var startEvents, endEvents []string
	callbacks := Callbacks{
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			cbMu.Lock()
			startEvents = append(startEvents, e.ToolCallID)
			cbMu.Unlock()
		},
		OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			cbMu.Lock()
			endEvents = append(endEvents, e.ToolCallID)
			cbMu.Unlock()
		},
	}

	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), Callbacks: callbacks,
	})

	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow tool never started executing")
	}

	// Both starts must have fired by the time the slow tool is merely
	// mid-execution — in real time, not deferred behind either outcome.
	deadline := time.After(2 * time.Second)
	for {
		cbMu.Lock()
		n := len(startEvents)
		cbMu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-deadline:
			cbMu.Lock()
			got := append([]string(nil), startEvents...)
			cbMu.Unlock()
			t.Fatalf("OnToolExecutionStart events while the slow tool is still running = %v, want both c-fast and c-slow", got)
		case <-time.After(5 * time.Millisecond):
		}
	}

	stream := out.Result.Stream()
	sawFastResult := false
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && c.ToolResult.ToolCallID == "c-fast" {
			sawFastResult = true
			// The fast tool's end callback must have already fired, and
			// the slow tool's must not have — it's still blocked on
			// releaseSlow at this very point in the stream.
			cbMu.Lock()
			got := append([]string(nil), endEvents...)
			cbMu.Unlock()
			if len(got) != 1 || got[0] != "c-fast" {
				t.Fatalf("OnToolExecutionEnd events when the fast tool's result streamed = %v, want exactly [c-fast] (not delayed behind the slow tool)", got)
			}
			close(releaseSlow)
		}
	}
	<-out.Done
	if !sawFastResult {
		t.Fatal("the fast tool's tool-result chunk never streamed")
	}
}
