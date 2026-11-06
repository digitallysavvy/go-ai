package claudecode_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// r85carryStarts records every start frame the fake bridge receives.
type r85carryStarts struct {
	mu     sync.Mutex
	frames []map[string]any
	got    chan struct{}
}

func newR85carryStarts() *r85carryStarts { return &r85carryStarts{got: make(chan struct{}, 8)} }

func (r *r85carryStarts) onStart(_ *bridgetest.Turn, start map[string]any) {
	r.mu.Lock()
	r.frames = append(r.frames, start)
	r.mu.Unlock()
	r.got <- struct{}{}
}

func (r *r85carryStarts) last(t *testing.T) map[string]any {
	t.Helper()
	select {
	case <-r.got:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames[len(r.frames)-1]
}

// r85carryJSON round-trips v through JSON so it compares with a decoded frame.
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

func r85carryExpect(t *testing.T, start map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got := start[k]; !reflect.DeepEqual(got, r85carryJSON(t, v)) {
			t.Errorf("start[%q] = %#v, want %#v", k, got, r85carryJSON(t, v))
		}
	}
}

func r85carryBool(b bool) *bool        { return &b }
func r85carryFloat(f float64) *float64 { return &f }
func r85carryInt(i int) *int           { return &i }

func r85carrySettings() claudecode.Settings {
	return claudecode.Settings{
		StrictMCPConfig: r85carryBool(true), MaxBudgetUSD: r85carryFloat(0.5),
		QuestionTimeoutMs: r85carryInt(600_000), ExcludeDynamicSections: r85carryBool(true),
		SkillOverrides: map[string]claudecode.SkillOverride{
			"update-config": claudecode.SkillOverrideOff, "fewer-permission-prompts": claudecode.SkillOverrideOff, "init": claudecode.SkillOverrideOff,
		},
		ToolHints: map[string]claudecode.ToolHint{"search": {ReadOnly: r85carryBool(true), AlwaysLoad: r85carryBool(true)}},
	}
}

var r85carryWantSettings = map[string]any{
	"strictMcpConfig": true, "maxBudgetUsd": 0.5, "questionTimeoutMs": 600000, "excludeDynamicSections": true,
	"skillOverrides": map[string]any{"update-config": "off", "fewer-permission-prompts": "off", "init": "off"},
	"toolHints":      map[string]any{"search": map[string]any{"readOnly": true, "alwaysLoad": true}},
}

func r85carryHarness(t *testing.T, settings claudecode.Settings, starts *r85carryStarts) (*claudecode.Harness, *testSandbox) {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: starts.onStart})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	settings.MintBridgeToken = func(string) string { return "tok" }
	settings.StartupTimeout = 2 * time.Second
	h, err := claudecode.New(settings)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, sandbox
}

// TS: "sends strict MCP config and the budget limit to the bridge", "sends
// skill overrides and host tool hints to the bridge", "sends the question
// timeout and excludeDynamicSections to the bridge" (claude-code-harness.test.ts,
// 09b3e1d166 / 6e3e99969d / 4ab93bbc91 / 31d83e2bd9).
func TestR85carryPromptStartSendsNewSettings(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, r85carrySettings(), starts)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hello"), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	start := starts.last(t)
	r85carryExpect(t, start, r85carryWantSettings)
	if _, ok := start["deferredToolUseId"]; ok {
		t.Errorf("a fresh prompt start sent deferredToolUseId = %v", start["deferredToolUseId"])
	}
}

// TS: "omits skill overrides when they are not configured".
func TestR85carryPromptStartOmitsUnsetSettings(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{}, starts)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("inspect the project"), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	start := starts.last(t)
	for _, k := range []string{"skillOverrides", "maxBudgetUsd", "strictMcpConfig", "questionTimeoutMs", "excludeDynamicSections", "toolHints", "readableSkillRoots", "deferredToolUseId"} {
		if _, ok := start[k]; ok {
			t.Errorf("start has %q = %v, want it omitted", k, start[k])
		}
	}
}

// TS: an explicit false is sent (`strictMcpConfig !== undefined`), which the
// pointer fields keep apart from unset.
func TestR85carryExplicitFalseIsSent(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{StrictMCPConfig: r85carryBool(false), ExcludeDynamicSections: r85carryBool(false), MaxBudgetUSD: r85carryFloat(0)}, starts)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	r85carryExpect(t, starts.last(t), map[string]any{"strictMcpConfig": false, "excludeDynamicSections": false, "maxBudgetUsd": 0})
}

// TS e3f24b634c (security): "writes standard Claude skill files and enables
// their names on start" now also expects readableSkillRoots, one per skill,
// `${sandboxHomeDir}/.claude/skills/${name}`.
func TestR85carryReadableSkillRoots(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{}, starts)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	skills := []harness.Skill{
		{Name: "weather-forecast", Description: "Forecasts.", Content: "Use it."},
		{Name: "weather-codes", Description: "Codes.", Content: "Use them."},
	}
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("weather"), TurnSettings: harness.TurnSettings{Skills: skills}, Emit: func(harness.StreamPart) {},
	}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	r85carryExpect(t, starts.last(t), map[string]any{
		"skills":             []string{"weather-forecast", "weather-codes"},
		"readableSkillRoots": []string{"/home/agent/.claude/skills/weather-forecast", "/home/agent/.claude/skills/weather-codes"},
	})
}

// TS: "sends ... when rerunning a continued turn" (env, tool hints, skill
// overrides, strict MCP config, budget, question timeout,
// excludeDynamicSections) and ede06e32cf "resumes a deferred approval when
// rerunning a continued turn" for continueFrom and resumeFrom.continueFrom.
func TestR85carryRerunContinueStart(t *testing.T) {
	pending := []harness.PendingToolApproval{
		{ApprovalID: "custom-1", ToolCallID: "custom-1", ToolName: "lookup", Input: "{}", Kind: "custom"},
		{ApprovalID: "call-1", ToolCallID: "call-1", ToolName: "bash", Input: `{"command":"pwd"}`, Kind: "builtin", ProviderExecuted: r85carryBool(true), NativeName: "Bash"},
		{ApprovalID: "call-2", ToolCallID: "call-2", ToolName: "write", Input: "{}", Kind: "builtin"},
	}
	for _, lifecycle := range []string{"continueFrom", "resumeFrom"} {
		t.Run(lifecycle, func(t *testing.T) {
			starts := newR85carryStarts()
			settings := r85carrySettings()
			settings.Env = map[string]string{"DEPLOYMENT_ENV": "staging"}
			h, sandbox := r85carryHarness(t, settings, starts)
			continueFrom, err := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			continueFrom.PendingToolApprovals = pending
			opts := harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", PermissionMode: harness.PermissionModeAllowReads}
			if lifecycle == "continueFrom" {
				opts.ContinueFrom = continueFrom
			} else {
				resumeFrom, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{})
				if err != nil {
					t.Fatal(err)
				}
				resumeFrom.ContinueFrom = continueFrom
				opts.ResumeFrom = resumeFrom
			}
			sess, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatalf("DoStart: %v", err)
			}
			t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
			if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
				t.Fatalf("DoContinueTurn: %v", err)
			}
			want := map[string]any{"env": map[string]string{"DEPLOYMENT_ENV": "staging"}, "continue": true, "deferredToolUseId": "call-1"}
			for k, v := range r85carryWantSettings {
				want[k] = v
			}
			r85carryExpect(t, starts.last(t), want)
		})
	}
}

// A rerun with no built-in pending approval sends no deferredToolUseId (TS
// `find(...)?.toolCallId` is undefined and the spread is skipped).
func TestR85carryRerunWithoutBuiltinApproval(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{}, starts)
	continueFrom, err := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{"claudeSessionId": "claude-1"})
	if err != nil {
		t.Fatal(err)
	}
	continueFrom.PendingToolApprovals = []harness.PendingToolApproval{{ApprovalID: "c", ToolCallID: "c", ToolName: "lookup", Input: "{}", Kind: "custom"}}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ContinueFrom: continueFrom})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoContinueTurn: %v", err)
	}
	start := starts.last(t)
	if start["resumeSessionId"] != "claude-1" {
		t.Errorf("resumeSessionId = %v, want claude-1", start["resumeSessionId"])
	}
	if _, ok := start["deferredToolUseId"]; ok {
		t.Errorf("deferredToolUseId = %v, want it omitted", start["deferredToolUseId"])
	}
}

// TS: "sends strict MCP config and the budget limit after attaching" and
// "sends the question timeout and excludeDynamicSections after attaching".
func TestR85carryAttachedSessionSendsNewSettings(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, r85carrySettings(), starts)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: resumeFrom})
	if err != nil {
		t.Fatalf("DoStart (attach): %v", err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount = %d, want 1 (attached)", sandbox.SpawnCount())
	}
	if _, err := attached.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hello again"), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	r85carryExpect(t, starts.last(t), r85carryWantSettings)
}
