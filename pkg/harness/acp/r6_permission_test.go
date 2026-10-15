package acp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func startFrameFor(t *testing.T, pm *PermissionModeMapping) map[string]any {
	t.Helper()
	captured := make(chan map[string]any, 1)
	srv := newServer(t, "tok", func(turn *bridgetest.Turn, start map[string]any) {
		captured <- start
		turn.Emit(finishFrame())
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.PermissionModeMapping = pm
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	return <-captured
}

// TS `isCompletePermissionModeMapping`: a partial mapping is ignored (never
// sent to the bridge); a complete one, with explicit null no-ops, is sent.
func TestPermissionModeMapping_PartialIsNotSentToBridge(t *testing.T) {
	target := &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full"}
	partial := &PermissionModeMapping{AllowAll: target, UnsetAllowReads: true, UnsetAllowEdits: true}
	if got := startFrameFor(t, partial); got["permissionModeMapping"] != nil {
		t.Fatalf("partial mapping sent: %v", got["permissionModeMapping"])
	}
	complete := &PermissionModeMapping{AllowAll: target}
	got, _ := startFrameFor(t, complete)["permissionModeMapping"].(map[string]any)
	if got == nil || got["allow-reads"] != nil || got["allow-all"] == nil {
		t.Fatalf("complete mapping = %v", got)
	}
}

func TestPermissionModeMapping_PartialWireAndIdentity(t *testing.T) {
	target := &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full"}
	partial := PermissionModeMapping{AllowAll: target, UnsetAllowReads: true}
	b, _ := json.Marshal(partial)
	if want := `{"allow-edits":null,"allow-all":{"type":"session-mode","modeId":"full"}}`; string(b) != want {
		t.Fatalf("got %s want %s", b, want)
	}
	var back PermissionModeMapping
	if err := json.Unmarshal(b, &back); err != nil || !back.UnsetAllowReads || back.UnsetAllowEdits || back.UnsetAllowAll || back.AllowAll == nil {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	if partial.IsComplete() || !(&PermissionModeMapping{}).IsComplete() || (*PermissionModeMapping)(nil).IsComplete() {
		t.Fatal("IsComplete")
	}
	if partial.Get(harness.PermissionModeAllowReads) != nil {
		t.Fatal("unset mode must not resolve")
	}
	// The identity covers the partial mapping (TS hashes the raw setting) and
	// differs from both "none" and the complete mapping.
	id := func(m *PermissionModeMapping) string {
		return createImplementationIdentity(implementationIdentityInput{PermissionModeMapping: m})
	}
	if id(&partial) == id(nil) || id(&partial) == id(&PermissionModeMapping{AllowAll: target}) {
		t.Fatal("identity ignores partial mapping")
	}
}

// v0.5.2 callers compare mappings with ==; the Unset flags must keep that valid.
func TestPermissionModeMapping_Comparable(t *testing.T) {
	a := PermissionModeMapping{UnsetAllowReads: true}
	b := PermissionModeMapping{UnsetAllowReads: true}
	if a != b || a == (PermissionModeMapping{}) {
		t.Fatal("comparison")
	}
}
