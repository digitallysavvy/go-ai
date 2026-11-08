package opencode

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func r88toolsAwait(t *testing.T, c harness.PromptControl) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish")
	}
}

// TS 3af9d06557: every prompt start frame carries readableSkillRoots (`[]`
// without skills, opencode-harness.ts 1035-1037); compact never does.
func TestR88toolsOpenCodeReadableSkillRoots(t *testing.T) {
	const token = "r88-roots-token"
	starts := make(chan map[string]any, 4)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		starts <- start
		turn.Emit(finishPayload())
	})
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/vercel/sandbox/s", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Skills: []harness.Skill{{Name: "demo", Description: "Demo skill.", Content: "Read reference.md."}}},
		Prompt:       harness.TextPrompt("Use demo."), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	r88toolsAwait(t, c)
	if got := (<-starts)["readableSkillRoots"]; !reflect.DeepEqual(got, []any{"/home/vercel-sandbox/.agents/skills/demo"}) {
		t.Fatalf("readableSkillRoots = %#v", got)
	}

	c, err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	r88toolsAwait(t, c)
	if got, ok := (<-starts)["readableSkillRoots"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("readableSkillRoots without skills = %#v (present %v), want []", got, ok)
	}

	if err := sess.DoCompact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got, ok := (<-starts)["readableSkillRoots"]; ok {
		t.Fatalf("compact start carries readableSkillRoots %#v", got)
	}
}
