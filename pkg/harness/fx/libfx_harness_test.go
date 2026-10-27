package fx

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports of TS fx-harness.test.ts on the mocked runtime.

func doTurn(t *testing.T, s harness.Session, opts harness.PromptTurnOptions) error {
	t.Helper()
	if opts.Emit == nil {
		opts.Emit = func(harness.StreamPart) {}
	}
	c, err := s.DoPromptTurn(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return waitDone(t, c)
}

func TestLibfxConstructsWithoutBridgeOrBootstrap(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	if h.HarnessID() != "fx" || h.SpecificationVersion() != "harness-v1" {
		t.Errorf("id %q spec %q", h.HarnessID(), h.SpecificationVersion())
	}
	if _, ok := harness.Harness(h).(harness.BootstrapProvider); ok {
		t.Error("the libfx harness declares a bootstrap")
	}
	if !harness.SupportsBuiltinToolApprovals(h) || !harness.SupportsBuiltinToolFiltering(h) {
		t.Error("approvals and filtering must be supported")
	}
	var names []string
	for _, n := range libfxBuiltinToolNames {
		if _, ok := h.BuiltinTools()[n]; ok {
			names = append(names, n)
		}
	}
	if want := []string{"read", "write", "edit", "bash", "grep", "glob", "ls", "askUserQuestions"}; !reflect.DeepEqual(names, want) || len(h.BuiltinTools()) != len(want) {
		t.Errorf("builtin tools = %v (%d)", names, len(h.BuiltinTools()))
	}
	if len(m.opts()) != 0 {
		t.Error("constructing started a runtime")
	}
}

func TestLibfxConstructsWithoutCredentials(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	h, err := CreateFx(Settings{Runtime: RuntimeLibfx})
	if err != nil || h.HarnessID() != "fx" {
		t.Fatalf("CreateFx: %v", err)
	}
}

func TestLibfxReappliesPerTurnSettings(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(sb, "settings"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("first")}); err != nil {
		t.Fatal(err)
	}
	if err := doTurn(t, s, harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{
			Model:        "anthropic/claude-haiku-4-5",
			Instructions: "new instructions",
			Skills:       []harness.Skill{{Name: "guide", Description: "guide", Content: "skill body", Files: []harness.SkillFile{{Path: "reference.md", Content: "resource body"}}}},
		},
		Prompt: harness.TextPrompt("second"),
	}); err != nil {
		t.Fatal(err)
	}
	opts := m.opts()
	if opts[0].Model != "" || opts[1].Model != "anthropic/claude-haiku-4-5" {
		t.Errorf("models = %q, %q", opts[0].Model, opts[1].Model)
	}
	for _, want := range []string{"new instructions", "resource body", "All filesystem and shell tools operate in the sandbox workspace /workspace."} {
		if !strings.Contains(opts[1].Instructions, want) {
			t.Errorf("instructions lack %q: %s", want, opts[1].Instructions)
		}
	}
	if string(opts[1].Checkpoint) != "\x01\x02\x03" {
		t.Errorf("checkpoint = %v", opts[1].Checkpoint)
	}
	if err := s.DoCompact(ctx, ""); !harness.IsCapabilityUnsupportedError(err) || !strings.Contains(err.Error(), "manual context compaction") {
		t.Errorf("DoCompact = %v", err)
	}
	if _, err := s.DoStop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLibfxForwardsInlineImagePrompts(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	s, err := h.DoStart(context.Background(), startOpts(sb, "image-input"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	msg := &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "What is in this image?"},
		types.FileContent{MediaType: "image/png", Data: []byte{1, 2, 3}},
	}}
	if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.Prompt{Message: msg}}); err != nil {
		t.Fatal(err)
	}
	want := []libfx.PromptBlock{{Type: "text", Text: "What is in this image?"}, {Type: "image", Data: "AQID", MimeType: "image/png"}}
	if !reflect.DeepEqual(m.prompts[0], want) {
		t.Errorf("prompt = %+v", m.prompts[0])
	}
}

func TestLibfxMapsServiceTiers(t *testing.T) {
	type flags struct{ fast, ultrafast *bool }
	var got []flags
	for _, tier := range []string{"", "default", "fast", "ultrafast"} {
		h, m := newMockHarness(t, Settings{ServiceTier: tier})
		s, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "service-tier"))
		if err != nil {
			t.Fatal(err)
		}
		if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go")}); err != nil {
			t.Fatal(err)
		}
		_ = s.DoDestroy(context.Background())
		o := m.opts()[0]
		got = append(got, flags{o.Fast, o.Ultrafast})
	}
	str := func(b *bool) string {
		if b == nil {
			return "undefined"
		}
		if *b {
			return "true"
		}
		return "false"
	}
	var parts []string
	for _, f := range got {
		parts = append(parts, str(f.fast)+"/"+str(f.ultrafast))
	}
	if want := "undefined/undefined,undefined/undefined,true/false,false/true"; strings.Join(parts, ",") != want {
		t.Errorf("tiers = %s", strings.Join(parts, ","))
	}
}

func toolNames(o runtimeOptions) []string {
	names := []string{}
	for _, tool := range o.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestLibfxFiltersToolsAndPreservesRecursiveSchemas(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	start := startOpts(newFakeSandbox(), "filter")
	start.BuiltinToolFiltering = &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow, ToolNames: []string{"read"}}
	s, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"rows": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": []any{"a", "b"}}}}}}}
	if err := doTurn(t, s, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom", InputSchema: schema}}}, Prompt: harness.TextPrompt("go")}); err != nil {
		t.Fatal(err)
	}
	o := m.opts()[0]
	if !reflect.DeepEqual(toolNames(o), []string{"read", "custom"}) {
		t.Errorf("tools = %v", toolNames(o))
	}
	for _, tool := range o.Tools {
		if tool.Idempotent {
			t.Errorf("%s is idempotent", tool.Name)
		}
	}
	if !reflect.DeepEqual(o.Tools[1].InputSchema, schema) {
		t.Errorf("schema = %v", o.Tools[1].InputSchema)
	}
}

func TestLibfxRegistersIncludedBuiltins(t *testing.T) {
	all := []string{"read", "write", "edit", "bash", "grep", "glob", "ls", "askUserQuestions"}
	cases := []struct {
		filtering *harness.BuiltinToolFiltering
		names     []string
	}{
		{nil, all},
		{&harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow}, []string{}},
		{&harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny}, all},
		{&harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{"bash", "write", "edit"}}, []string{"read", "grep", "glob", "ls", "askUserQuestions"}},
	}
	for _, c := range cases {
		h, m := newMockHarness(t, Settings{})
		start := startOpts(newFakeSandbox(), "filtering")
		start.BuiltinToolFiltering = c.filtering
		s, err := h.DoStart(context.Background(), start)
		if err != nil {
			t.Fatal(err)
		}
		if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go")}); err != nil {
			t.Fatal(err)
		}
		if got := toolNames(m.opts()[0]); !reflect.DeepEqual(got, c.names) {
			t.Errorf("%+v: tools = %v", c.filtering, got)
		}
		_ = s.DoDestroy(context.Background())
	}
}

func TestLibfxCustomToolOverridesFilteredBuiltin(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	start := startOpts(newFakeSandbox(), "override")
	start.BuiltinToolFiltering = &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow}
	s, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	if err := doTurn(t, s, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "read", Description: "custom read"}}}, Prompt: harness.TextPrompt("go")}); err != nil {
		t.Fatal(err)
	}
	o := m.opts()[0]
	if len(o.Tools) != 1 || o.Tools[0].Name != "read" || o.Tools[0].Description != "custom read" {
		t.Errorf("tools = %+v", o.Tools)
	}
}

func TestLibfxColdContinuesPendingCustomCall(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	var mu sync.Mutex
	var captured *libfx.TypedToolResult
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		v, err := invoke(o, "custom", "original-id", map[string]any{"value": strings.Repeat("x", 70000)})
		if err != nil {
			return err
		}
		mu.Lock()
		captured = v.(*libfx.TypedToolResult)
		mu.Unlock()
		return nil
	})
	start := startOpts(sb, "resume")
	opts := harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}}
	s, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	waiting := newSignal()
	events := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("go"), Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	state, err := s.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	if n := len(events.ofType(harness.PartTypeToolCall)); n != 1 {
		t.Fatalf("tool calls = %d", n)
	}
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: state})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	after := &partLog{}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	err = continued.SubmitToolResult(ctx, harness.ToolResultSubmission{
		ToolCallID: "original-id",
		Output:     map[string]any{"visible": true},
		ToolResult: &harness.ModelToolResult{Type: "tool-result", ToolCallID: "original-id", ToolName: "custom", Output: textOutput(strings.Repeat("full-output", 20000))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if captured == nil || len(captured.Text) != 220000 {
		t.Errorf("captured text length = %v", captured)
	}
	mu.Unlock()
	if n := len(after.ofType(harness.PartTypeToolCall)); n != 0 {
		t.Errorf("continuation re-emitted %d tool calls", n)
	}
	results := after.ofType(harness.PartTypeToolResult)
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	want := &harness.ToolResultPart{ToolCallID: "original-id", ToolName: "custom", Result: map[string]any{"visible": true}}
	if !reflect.DeepEqual(results[0], want) {
		t.Errorf("tool-result = %+v", results[0])
	}
}

func realpathSandbox(sb *fakeSandbox, run func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error)) {
	sb.handle = func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "realpath") {
			out := "/workspace\x00"
			if strings.Contains(opts.Command, "notes.md") {
				out = "/workspace/notes.md\x00"
			}
			return providerutils.SandboxRunResult{Stdout: b64(out)}, nil
		}
		if run != nil {
			return run(ctx, opts)
		}
		return providerutils.SandboxRunResult{}, nil
	}
}

func TestLibfxReportsMutationsThroughToolEvents(t *testing.T) {
	cases := []struct {
		name   string
		input  map[string]any
		inputJ string
		result any
	}{
		{"write", map[string]any{"file_path": "notes.md", "content": "hello"}, `{"content":"hello","file_path":"notes.md"}`, "Wrote notes.md"},
		{"edit", map[string]any{"file_path": "notes.md", "old_string": "hello", "new_string": "Hello"}, `{"file_path":"notes.md","new_string":"Hello","old_string":"hello"}`, "Edited notes.md"},
		{"bash", map[string]any{"command": "touch important"}, `{"command":"touch important"}`, map[string]any{"exitCode": float64(0), "stdout": "", "stderr": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, m := newMockHarness(t, Settings{})
			sb := newFakeSandbox()
			sb.files["/workspace/notes.md"] = "hello"
			realpathSandbox(sb, nil)
			m.setRun(func(_ context.Context, o runtimeOptions) error {
				_, err := invoke(o, c.name, "mutation-id", c.input)
				return err
			})
			s, err := h.DoStart(context.Background(), startOpts(sb, "mutation"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DoDestroy(context.Background()) //nolint:errcheck
			events := &partLog{}
			if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: events.emit}); err != nil {
				t.Fatal(err)
			}
			if len(events.ofType(harness.PartTypeFileChange)) != 0 {
				t.Error("file-change emitted")
			}
			call := events.ofType(harness.PartTypeToolCall)[0].(*harness.ToolCallPart)
			if call.ToolCallID != "mutation-id" || call.ToolName != c.name || call.Input != c.inputJ || call.NativeName != c.name || !call.ProviderExecuted {
				t.Errorf("tool-call = %+v", call)
			}
			res := events.ofType(harness.PartTypeToolResult)[0].(*harness.ToolResultPart)
			if !reflect.DeepEqual(res.Result, c.result) || res.IsError {
				t.Errorf("tool-result = %+v", res)
			}
			var other []string
			for _, cmd := range sb.commandList() {
				if !strings.Contains(cmd, `"$HOME"`) && !strings.HasPrefix(cmd, "mkdir -p --") && !strings.HasPrefix(cmd, "mv --") && !strings.Contains(cmd, "realpath") {
					other = append(other, cmd)
				}
			}
			wantOther := []string(nil)
			if c.name == "bash" {
				wantOther = []string{"touch important"}
			}
			if !reflect.DeepEqual(other, wantOther) {
				t.Errorf("sandbox commands = %v", other)
			}
		})
	}
}

func TestLibfxGatesShellBeforeTouchingSandbox(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		_, _ = invoke(o, "bash", "shell-id", map[string]any{"command": "touch important"})
		return nil
	})
	start := startOpts(sb, "approval")
	start.PermissionMode = harness.PermissionModeAllowReads
	s, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	approval := newSignal()
	control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeToolApprovalRequest {
			approval.fire()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	approval.wait(t)
	touched := func() bool {
		for _, c := range sb.commandList() {
			if strings.Contains(c, "touch important") {
				return true
			}
		}
		return false
	}
	if touched() {
		t.Fatal("ran before approval")
	}
	if err := control.(harness.ToolApprovalSubmitter).SubmitToolApproval(context.Background(), harness.ToolApprovalSubmission{ApprovalID: "fx:shell-id", Approved: false, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	if touched() {
		t.Error("denied command ran")
	}
}

func TestLibfxReattachesLivePendingTool(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		_, err := invoke(o, "custom", "live-id", map[string]any{})
		return err
	})
	s, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "live"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	opts := harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}}
	waiting := newSignal()
	before := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("go"), Emit: before.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	after := &partLog{}
	continued, err := s.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if continued != control {
		t.Fatal("continuing a live turn returned a new control")
	}
	if err := continued.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "live-id", Output: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatal(err)
	}
	if len(m.opts()) != 1 {
		t.Errorf("runtimes started = %d", len(m.opts()))
	}
	if len(before.ofType(harness.PartTypeToolResult)) != 0 {
		t.Error("result went to the old emitter")
	}
	res := after.ofType(harness.PartTypeToolResult)
	if len(res) != 1 || !reflect.DeepEqual(res[0], &harness.ToolResultPart{ToolCallID: "live-id", ToolName: "custom", Result: "ok"}) {
		t.Errorf("results = %+v", res)
	}
}

func TestLibfxDoesNotReexecuteInterruptedShellCall(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	executing := newSignal()
	var mu sync.Mutex
	runs := 0
	realpathSandbox(sb, func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if opts.Command != "long-running-effect" {
			return providerutils.SandboxRunResult{}, nil
		}
		mu.Lock()
		runs++
		mu.Unlock()
		executing.fire()
		<-ctx.Done()
		return providerutils.SandboxRunResult{}, ctx.Err()
	})
	var errs []string
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		if _, err := invoke(o, "bash", "interrupted-id", map[string]any{"command": "long-running-effect"}); err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
		}
		return nil
	})
	start := startOpts(sb, "interrupted")
	s, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	executing.wait(t)
	continueFrom, err := s.DoSuspendTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	resumed, err := h.DoStart(context.Background(), harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(context.Background()) //nolint:errcheck
	events := &partLog{}
	c2, err := resumed.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, c2)
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("effect ran %d times", runs)
	}
	if len(errs) == 0 || !strings.Contains(errs[len(errs)-1], "its outcome is unknown") {
		t.Errorf("errors = %v", errs)
	}
	res := events.ofType(harness.PartTypeToolResult)
	want := &harness.ToolResultPart{ToolCallID: "interrupted-id", ToolName: "bash", Result: interruptedToolText, IsError: true}
	if len(res) != 1 || !reflect.DeepEqual(res[0], want) {
		t.Errorf("results = %+v", res)
	}
}

type failurePhase string

func TestLibfxTurnFailuresPreserveCheckpoint(t *testing.T) {
	for _, phase := range []failurePhase{"initialization", "prompt", "abort", "pre-aborted"} {
		t.Run(string(phase), func(t *testing.T) {
			h, m := newMockHarness(t, Settings{})
			sb := newFakeSandbox()
			ctx := context.Background()
			s, err := h.DoStart(ctx, startOpts(sb, "failed-turn"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DoDestroy(ctx) //nolint:errcheck
			if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("baseline")}); err != nil {
				t.Fatal(err)
			}
			_, baselineText := sb.stateFile(t)
			var baseline fxStoredState
			_ = json.Unmarshal([]byte(baselineText), &baseline)
			failure := errors.New(string(phase) + " failed")
			turnCtx, cancel := context.WithCancelCause(ctx)
			defer cancel(nil)
			switch phase {
			case "initialization":
				m.mu.Lock()
				m.startErr = failure
				m.mu.Unlock()
			case "pre-aborted":
				cancel(failure)
			case "abort":
				m.setRun(func(context.Context, runtimeOptions) error { cancel(failure); return nil })
			default:
				m.setRun(func(context.Context, runtimeOptions) error { return failure })
			}
			events := &partLog{}
			c, err := s.DoPromptTurn(turnCtx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("fail"), Emit: events.emit})
			if err != nil {
				t.Fatal(err)
			}
			if err := waitDone(t, c); err != failure {
				t.Fatalf("done = %v", err)
			}
			errsParts := events.ofType(harness.PartTypeError)
			if len(errsParts) != 1 || errsParts[0].(*harness.ErrorPart).Error != failure {
				t.Errorf("error parts = %+v", errsParts)
			}
			if len(events.ofType(harness.PartTypeFinish)) != 0 {
				t.Error("finish emitted")
			}
			_, storedText := sb.stateFile(t)
			var stored fxStoredState
			_ = json.Unmarshal([]byte(storedText), &stored)
			if stored.Turn != nil || stored.Checkpoint != baseline.Checkpoint {
				t.Errorf("stored state = %s", storedText)
			}
			if _, err := s.DoContinueTurn(ctx, harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err == nil || err.Error() != "There is no suspended fx turn to continue." {
				t.Errorf("continue = %v", err)
			}
			m.setRun(nil)
			if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("retry")}); err != nil {
				t.Fatal(err)
			}
			opts := m.opts()
			if string(opts[len(opts)-1].Checkpoint) != "\x01\x02\x03" {
				t.Error("retry did not restore the checkpoint")
			}
		})
	}
}

func TestLibfxCleanupFailureClearsActiveTurn(t *testing.T) {
	for _, promptFailed := range []bool{false, true} {
		h, m := newMockHarness(t, Settings{})
		s, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "cleanup-failed"))
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("Prompt failed")
		cleanupFailure := errors.New("Runtime close failed")
		m.setRun(func(context.Context, runtimeOptions) error {
			if promptFailed {
				return failure
			}
			return nil
		})
		m.mu.Lock()
		m.close = func() error { return cleanupFailure }
		m.mu.Unlock()
		events := &partLog{}
		if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: events.emit}); err != cleanupFailure {
			t.Errorf("done = %v", err)
		}
		if promptFailed {
			if e := events.ofType(harness.PartTypeError); len(e) != 1 || e[0].(*harness.ErrorPart).Error != failure {
				t.Errorf("errors = %+v", e)
			}
		}
		m.mu.Lock()
		m.close = nil
		m.mu.Unlock()
		stopped, err := s.DoStop(context.Background())
		if err != nil || stopped.ContinueFrom != nil {
			t.Errorf("stop = %+v, %v", stopped, err)
		}
	}
}

func TestLibfxFreshPromptAfterFailedColdContinuation(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	start := startOpts(sb, "failed-continuation")
	s, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	opts := harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}}
	waiting := newSignal()
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		_, err := invoke(o, "custom", "pending-id", map[string]any{})
		return err
	})
	first, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("wait"), Emit: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	if _, err := s.DoSuspendTurn(ctx); err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, first)
	resumeFrom, err := s.DoStop(ctx)
	if err != nil || resumeFrom.ContinueFrom == nil {
		t.Fatalf("stop = %+v, %v", resumeFrom, err)
	}
	failure := errors.New("Continuation failed")
	m.setRun(func(context.Context, runtimeOptions) error { return failure })
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ResumeFrom: resumeFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != failure {
		t.Fatalf("continued = %v", err)
	}
	if _, stored := sb.stateFile(t); strings.Contains(stored, `"turn"`) {
		t.Errorf("turn kept after failure: %s", stored)
	}
	m.setRun(nil)
	if err := doTurn(t, resumed, harness.PromptTurnOptions{Prompt: harness.TextPrompt("fresh")}); err != nil {
		t.Fatal(err)
	}
}

// isolateHome points subscription discovery at an empty home, as the TS
// suite mocks os.homedir.
func isolateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
}

func TestLibfxGatewayAuthMissingFailsBeforeSandboxAccess(t *testing.T) {
	isolateHome(t)
	for _, key := range []string{"unset", ""} {
		t.Setenv("VERCEL_OIDC_TOKEN", "")
		t.Setenv("AI_GATEWAY_API_KEY", "")
		_ = key
		h, err := CreateFx(Settings{Runtime: RuntimeLibfx})
		if err != nil {
			t.Fatal(err)
		}
		sb := newFakeSandbox()
		_, err = h.DoStart(context.Background(), startOpts(sb, "missing-auth"))
		if err == nil || err.Error() != errNoGatewayCredential {
			t.Errorf("DoStart = %v", err)
		}
		if len(sb.commandList()) != 0 || len(sb.files) != 0 || len(sb.reads) != 0 {
			t.Error("touched the sandbox")
		}
	}
	h, err := CreateFx(Settings{Runtime: RuntimeLibfx, Auth: harness.AuthEnvironment(map[string]string{})})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_GATEWAY_API_KEY", "host-key")
	sb := newFakeSandbox()
	if _, err := h.DoStart(context.Background(), startOpts(sb, "isolated-auth")); err == nil || !strings.Contains(err.Error(), "fx requires AI_GATEWAY_API_KEY or VERCEL_OIDC_TOKEN") {
		t.Errorf("isolated empty auth = %v", err)
	}
	if len(sb.commandList()) != 0 {
		t.Error("touched the sandbox")
	}
}

// TestLibfxBrokersResolvedCredential mirrors TS "passes the resolved
// credential to the runtime": Go hands the runtime a placeholder and
// brokers the resolved credential on the network side.
func TestLibfxBrokersResolvedCredential(t *testing.T) {
	cases := []struct {
		auth     map[string]string
		expected string
	}{
		{map[string]string{"AI_GATEWAY_API_KEY": "explicit-key"}, "explicit-key"},
		{map[string]string{"VERCEL_OIDC_TOKEN": "explicit-oidc"}, "explicit-oidc"},
		{map[string]string{"AI_GATEWAY_API_KEY": "preferred-key", "VERCEL_OIDC_TOKEN": "oidc"}, "preferred-key"},
	}
	for _, c := range cases {
		h, m := newMockHarness(t, Settings{Auth: harness.AuthEnvironment(c.auth)})
		s, err := h.DoStart(context.Background(), startOpts(newFakeSandbox(), "auth"))
		if err != nil {
			t.Fatal(err)
		}
		if err := doTurn(t, s, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go")}); err != nil {
			t.Fatal(err)
		}
		ls := s.(*libfxSession)
		if ls.broker.real != c.expected {
			t.Errorf("brokered credential = %q, want %q", ls.broker.real, c.expected)
		}
		if got := m.opts()[0].APIKey; got != ls.broker.placeholder || strings.Contains(got, c.expected) {
			t.Errorf("runtime apiKey = %q", got)
		}
		_ = s.DoDestroy(context.Background())
	}
}

func resolveAuthentication(auth AuthenticationMode, required string, env map[string]string, home ...string) (fxAuthentication, error) {
	o := resolveAuthOptions{Auth: auth, Env: env, RequiredProvider: required, HomeDirectory: "/nonexistent-fx-test-home", Keyring: noKeyring{}}
	if len(home) > 0 {
		o.HomeDirectory = home[0]
	}
	return resolveFxAuthentication(context.Background(), o)
}

func TestResolveFxEnvModes(t *testing.T) {
	for _, mode := range []string{"auto", "direct", "ai-gateway"} {
		got, err := resolveAuthentication(harness.AuthMode(mode), "", map[string]string{"VERCEL_OIDC_TOKEN": "oidc"})
		if err != nil || got.APIKey != "oidc" {
			t.Errorf("%s: %+v, %v", mode, got, err)
		}
	}
	if _, err := resolveAuthentication(harness.AuthEnvironment(map[string]string{}), "", map[string]string{"AI_GATEWAY_API_KEY": "host-key"}); err == nil {
		t.Error("an explicit auth environment read the host key")
	}
	if _, err := resolveAuthentication(harness.AuthMode("ai-gateway"), "codex", nil); err == nil || !strings.Contains(err.Error(), "does not allow the saved subscription provider") {
		t.Errorf("pinned subscription under ai-gateway = %v", err)
	}
}

func TestLibfxRejectsNativeBackendAcceptsMCP(t *testing.T) {
	if _, err := CreateFx(Settings{Runtime: RuntimeLibfx, Backend: "native"}); !harness.IsCapabilityUnsupportedError(err) {
		t.Errorf("native backend = %v", err)
	}
	if _, err := CreateFx(Settings{Runtime: RuntimeLibfx, MCPServers: map[string]any{"s": map[string]any{"url": "https://x"}}}); err != nil {
		t.Errorf("mcp = %v", err)
	}
	if _, err := CreateFx(Settings{Runtime: "other"}); err == nil {
		t.Error("unknown runtime accepted")
	}
	for _, n := range []int64{-1, 4<<30 + 1, 8 << 30} {
		if _, err := CreateFx(Settings{Runtime: RuntimeLibfx, MemoryLimitBytes: n}); err == nil || !strings.Contains(err.Error(), "MemoryLimitBytes") {
			t.Errorf("MemoryLimitBytes %d = %v", n, err)
		}
	}
	if _, err := CreateFx(Settings{Runtime: RuntimeLibfx, MemoryLimitBytes: 4 << 30}); err != nil {
		t.Errorf("MemoryLimitBytes 4 GiB = %v", err)
	}
}
