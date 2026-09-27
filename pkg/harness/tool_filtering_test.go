package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func testToolSet(names ...string) map[string]types.Tool {
	out := make(map[string]types.Tool, len(names))
	for _, n := range names {
		out[n] = types.Tool{Name: n, Parameters: map[string]any{"type": "object"}}
	}
	return out
}

func mergedToolSet(sets ...map[string]types.Tool) map[string]types.Tool {
	out := map[string]types.Tool{}
	for _, s := range sets {
		for k, v := range s {
			out[k] = v
		}
	}
	return out
}

// TestResolveToolFiltering_RejectsBothActiveAndInactive ports TS
// tool-filtering.ts's own guard (exercised end to end by
// harness-agent.test.ts's "rejects activeTools and inactiveTools together at
// runtime").
func TestResolveToolFiltering_RejectsBothActiveAndInactive(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock"}
	_, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, ActiveTools: []string{}, InactiveTools: []string{},
	})
	if err == nil || err.Error() != "HarnessAgent: pass either `activeTools` or `inactiveTools`, not both." {
		t.Fatalf("err = %v, want the both-together error", err)
	}
}

// TestResolveToolFiltering_RejectsUnknownToolName ports "rejects unknown
// active tool names" (harness-agent.test.ts): an activeTools/inactiveTools
// entry naming a tool that doesn't exist in the merged user+builtin set is a
// NoSuchToolError, not silently ignored.
func TestResolveToolFiltering_RejectsUnknownToolName(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock"}
	all := testToolSet("echo")
	_, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, UserTools: all, AllTools: all, ActiveTools: []string{"missing"},
	})
	var noSuchTool *ai.NoSuchToolError
	if !errors.As(err, &noSuchTool) {
		t.Fatalf("err = %v (%T), want *ai.NoSuchToolError", err, err)
	}
	if noSuchTool.ToolName != "missing" {
		t.Fatalf("ToolName = %q, want %q", noSuchTool.ToolName, "missing")
	}
	if len(noSuchTool.AvailableTools) != 1 || noSuchTool.AvailableTools[0] != "echo" {
		t.Fatalf("AvailableTools = %v, want [echo]", noSuchTool.AvailableTools)
	}
}

// TestResolveToolFiltering_NoFilterPassesEverythingThrough verifies the
// default (both ActiveTools and InactiveTools nil): every user tool stays
// active and no BuiltinToolFiltering is computed, matching TS's `?? []`/
// unfiltered fallthrough.
func TestResolveToolFiltering_NoFilterPassesEverythingThrough(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock", builtinTools: map[string]BuiltinTool{
		"bash": {Tool: types.Tool{Name: "bash"}},
	}}
	user := testToolSet("echo", "hidden")
	got, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, UserTools: user, AllTools: mergedToolSet(user, testToolSet("bash")),
	})
	if err != nil {
		t.Fatalf("ResolveToolFiltering: %v", err)
	}
	if len(got.ActiveUserTools) != 2 {
		t.Fatalf("ActiveUserTools = %v, want both tools active", got.ActiveUserTools)
	}
	if got.BuiltinToolFiltering != nil {
		t.Fatalf("BuiltinToolFiltering = %+v, want nil", got.BuiltinToolFiltering)
	}
}

// TestResolveToolFiltering_ActiveToolsFiltersUserAndBuiltins ports
// "activeTools filters custom tool specs" at the resolver level: only the
// listed user tools stay active, and the builtin filtering policy becomes
// mode "allow" with exactly the listed builtin names (even when that list is
// empty, matching TS "passes builtin filtering policy to approval-capable
// harnesses": `{ mode: 'allow', toolNames: [] }`).
func TestResolveToolFiltering_ActiveToolsFiltersUserAndBuiltins(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock", supportsApproval: true, builtinTools: map[string]BuiltinTool{
		"bash": {Tool: types.Tool{Name: "bash"}},
	}}
	user := testToolSet("echo", "hidden")
	got, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, UserTools: user, AllTools: mergedToolSet(user, testToolSet("bash")),
		ActiveTools: []string{"echo"},
	})
	if err != nil {
		t.Fatalf("ResolveToolFiltering: %v", err)
	}
	if len(got.ActiveUserTools) != 1 || got.ActiveUserTools["echo"].Name != "echo" {
		t.Fatalf("ActiveUserTools = %v, want only echo", got.ActiveUserTools)
	}
	if got.BuiltinToolFiltering == nil || got.BuiltinToolFiltering.Mode != BuiltinToolFilteringAllow || len(got.BuiltinToolFiltering.ToolNames) != 0 {
		t.Fatalf("BuiltinToolFiltering = %+v, want {mode: allow, toolNames: []}", got.BuiltinToolFiltering)
	}
}

// TestResolveToolFiltering_InactiveToolsFiltersUserAndBuiltins ports
// "inactiveTools filters custom tool specs": the listed user tools are
// excluded, and the builtin filtering policy becomes mode "deny" with
// exactly the listed builtin names.
func TestResolveToolFiltering_InactiveToolsFiltersUserAndBuiltins(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock", supportsApproval: true, builtinTools: map[string]BuiltinTool{
		"bash": {Tool: types.Tool{Name: "bash"}},
	}}
	user := testToolSet("echo", "hidden")
	got, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, UserTools: user, AllTools: mergedToolSet(user, testToolSet("bash")),
		InactiveTools: []string{"hidden", "bash"},
	})
	if err != nil {
		t.Fatalf("ResolveToolFiltering: %v", err)
	}
	if len(got.ActiveUserTools) != 1 || got.ActiveUserTools["echo"].Name != "echo" {
		t.Fatalf("ActiveUserTools = %v, want only echo", got.ActiveUserTools)
	}
	if got.BuiltinToolFiltering == nil || got.BuiltinToolFiltering.Mode != BuiltinToolFilteringDeny || len(got.BuiltinToolFiltering.ToolNames) != 1 || got.BuiltinToolFiltering.ToolNames[0] != "bash" {
		t.Fatalf("BuiltinToolFiltering = %+v, want {mode: deny, toolNames: [bash]}", got.BuiltinToolFiltering)
	}
}

// TestResolveToolFiltering_DedupesToolNames verifies duplicate entries in
// ActiveTools/InactiveTools do not change the outcome, mirroring TS
// `dedupeToolNames`'s `Array.from(new Set(...))`.
func TestResolveToolFiltering_DedupesToolNames(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock"}
	user := testToolSet("echo")
	got, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, UserTools: user, AllTools: user,
		ActiveTools: []string{"echo", "echo", "echo"},
	})
	if err != nil {
		t.Fatalf("ResolveToolFiltering: %v", err)
	}
	if len(got.ActiveUserTools) != 1 {
		t.Fatalf("ActiveUserTools = %v, want exactly 1 entry", got.ActiveUserTools)
	}
}

// TestResolveToolFiltering_RejectsWhenHarnessCannotEnforceBuiltinFiltering
// ports "rejects builtin filtering when the harness cannot enforce it": a
// computed BuiltinToolFiltering against a harness that supports neither
// built-in tool filtering nor built-in tool approvals is a
// CapabilityUnsupportedError.
func TestResolveToolFiltering_RejectsWhenHarnessCannotEnforceBuiltinFiltering(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock", builtinTools: map[string]BuiltinTool{
		"bash": {Tool: types.Tool{Name: "bash"}},
	}}
	all := testToolSet("bash")
	_, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, AllTools: all, ActiveTools: []string{},
	})
	var capErr *CapabilityUnsupportedError
	if !errors.As(err, &capErr) {
		t.Fatalf("err = %v (%T), want *CapabilityUnsupportedError", err, err)
	}
}

// TestResolveToolFiltering_AllowsBuiltinFilteringWhenApprovalsSupported
// verifies a harness that supports built-in tool approvals (but not native
// filtering) is still allowed to receive a builtin filtering policy — the
// runtime enforces it itself via auto-deny on approval requests. Mirrors TS
// "passes builtin filtering policy to approval-capable harnesses".
func TestResolveToolFiltering_AllowsBuiltinFilteringWhenApprovalsSupported(t *testing.T) {
	h := &mockHarnessAdapter{id: "mock", supportsApproval: true, builtinTools: map[string]BuiltinTool{
		"bash": {Tool: types.Tool{Name: "bash"}},
	}}
	all := testToolSet("bash")
	got, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: h, AllTools: all, ActiveTools: []string{},
	})
	if err != nil {
		t.Fatalf("ResolveToolFiltering: %v", err)
	}
	if got.BuiltinToolFiltering == nil || got.BuiltinToolFiltering.Mode != BuiltinToolFilteringAllow {
		t.Fatalf("BuiltinToolFiltering = %+v, want {mode: allow, toolNames: []}", got.BuiltinToolFiltering)
	}
}

// TestAgent_ActiveToolsBlocksInactiveHostToolExecution ports "activeTools
// filters custom tool specs and blocks inactive custom execution" end to
// end: a tool call for a filtered-out user tool is submitted back to the
// harness as an execution-denied result instead of being executed, with the
// exact denial reason text.
func TestAgent_ActiveToolsBlocksInactiveHostToolExecution(t *testing.T) {
	var echoExecuted, hiddenExecuted bool
	echo := types.Tool{
		Name: "echo", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			echoExecuted = true
			return "echoed", nil
		},
	}
	hidden := types.Tool{
		Name: "hidden", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			hiddenExecuted = true
			return "hidden output", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "hidden", Input: `{"value":"ping"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, UserTools: map[string]types.Tool{"echo": echo, "hidden": hidden},
		ActiveTools: []string{"echo"},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if hiddenExecuted {
		t.Fatal("the filtered-out 'hidden' tool must not execute")
	}
	if echoExecuted {
		t.Fatal("'echo' was never called by the script; it should not have executed either")
	}
	if len(mock.toolResults) != 1 {
		t.Fatalf("toolResults = %+v, want 1", mock.toolResults)
	}
	got := mock.toolResults[0]
	if got.ToolCallID != "c1" {
		t.Fatalf("ToolCallID = %q, want c1", got.ToolCallID)
	}
	want := map[string]interface{}{
		"type":   "execution-denied",
		"reason": "Tool 'hidden' is inactive due to the HarnessAgent tool filtering policy.",
	}
	gotMap, ok := got.Output.(map[string]interface{})
	if !ok || gotMap["type"] != want["type"] || gotMap["reason"] != want["reason"] {
		t.Fatalf("Output = %#v, want %#v", got.Output, want)
	}
}

// TestAgent_RejectsBuiltinFilteringWhenHarnessCannotEnforceIt ports "rejects
// builtin filtering when the harness cannot enforce it" at the NewAgent
// level.
func TestAgent_RejectsBuiltinFilteringWhenHarnessCannotEnforceIt(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		builtinTools: map[string]BuiltinTool{"bash": {Tool: types.Tool{Name: "bash"}}},
		script:       finishEventsScript,
	})
	_, err := NewAgent(AgentSettings{Harness: mock.harness, ActiveTools: []string{}})
	var capErr *CapabilityUnsupportedError
	if !errors.As(err, &capErr) {
		t.Fatalf("NewAgent err = %v (%T), want *CapabilityUnsupportedError", err, err)
	}
}

// TestAgent_PassesBuiltinFilteringPolicyToApprovalCapableHarness ports
// "passes builtin filtering policy to approval-capable harnesses": DoStart
// receives BuiltinToolFiltering{Mode: allow, ToolNames: []} when ActiveTools
// is an empty (non-nil) slice and the harness supports builtin approvals.
func TestAgent_PassesBuiltinFilteringPolicyToApprovalCapableHarness(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		builtinTools:     map[string]BuiltinTool{"bash": {Tool: types.Tool{Name: "bash"}}},
		supportsApproval: true,
		script:           finishEventsScript,
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, ActiveTools: []string{}})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = session.Destroy(context.Background()) }()

	adapter := mock.harness.(*mockHarnessAdapter)
	if len(adapter.startCalls) != 1 {
		t.Fatalf("startCalls = %+v, want 1", adapter.startCalls)
	}
	got := adapter.startCalls[0].BuiltinToolFiltering
	if got == nil || got.Mode != BuiltinToolFilteringAllow || len(got.ToolNames) != 0 {
		t.Fatalf("BuiltinToolFiltering = %+v, want {mode: allow, toolNames: []}", got)
	}
}
