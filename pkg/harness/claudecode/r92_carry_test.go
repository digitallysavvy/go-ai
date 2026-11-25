package claudecode_test

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS claude-code-harness.test.ts (2ebc78b654) 'sends skill overrides and
// auto mode when rerunning a continued turn'.
func TestR92carryRerunSendsSkillOverridesAndAutoMode(t *testing.T) {
	starts := newR85carryStarts()
	auto := true
	h, sandbox := r85carryHarness(t, claudecode.Settings{
		SkillOverrides: map[string]claudecode.SkillOverride{"init": claudecode.SkillOverrideOff},
		AutoMode:       &auto,
	}, starts)
	continueFrom, err := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ContinueFrom: continueFrom})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoContinueTurn: %v", err)
	}
	r85carryExpect(t, starts.last(t), map[string]any{
		"skillOverrides": map[string]any{"init": "off"},
		"autoMode":       true,
		"continue":       true,
	})
}
