package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/bridgeauthkeys"
)

// TS c849d37dac acp-harness.test.ts: the sandbox gets only
// BRIDGE_AUTH_PUBLIC_KEY, resume state carries bridge.privateKey, the private
// key never reaches the sandbox, an attach reuses it, and legacy token state
// is rejected.
func TestR91toolsACPBridgePublicKeyAuth(t *testing.T) {
	key := bridgeauthkeys.Testing()
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	sandbox.mu.Lock()
	env := sandbox.spawnEnvs[0]
	all, _ := json.Marshal(map[string]any{"env": sandbox.spawnEnvs, "commands": sandbox.spawnCommands, "runs": sandbox.runs, "files": sandbox.files})
	sandbox.mu.Unlock()
	if env[bridge.EnvAuthPublicKey] != key.PublicKey {
		t.Errorf("BRIDGE_AUTH_PUBLIC_KEY = %q", env[bridge.EnvAuthPublicKey])
	}
	if _, ok := env["BRIDGE_CHANNEL_TOKEN"]; ok {
		t.Error("spawn env carries BRIDGE_CHANNEL_TOKEN")
	}
	if strings.Contains(string(all), key.PrivateKey) {
		t.Fatal("the bridge private key reached the sandbox")
	}
	resume, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	if !strings.Contains(string(resume.Data), `"privateKey":"`+key.PrivateKey+`"`) || strings.Contains(string(resume.Data), `"token"`) {
		t.Fatalf("resume data = %s", resume.Data)
	}
	v := h.(harness.LifecycleStateValidator)
	if err := v.ValidateLifecycleStateData(resume.Data); err != nil {
		t.Errorf("own resume state rejected: %v", err)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox, ResumeFrom: resume})
	if err != nil {
		t.Fatalf("DoStart (attach): %v", err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	sandbox.mu.Lock()
	n := len(sandbox.spawnEnvs)
	sandbox.mu.Unlock()
	if n != 1 {
		t.Fatalf("attach respawned the bridge (%d spawns)", n)
	}
	if err := v.ValidateLifecycleStateData(json.RawMessage(`{"agentIdentity":"i","bridge":{"port":4319,"token":"legacy-token","lastSeenEventId":0}}`)); err == nil {
		t.Fatal("legacy token state accepted")
	}
}

// TS "ignores a mintBridgeToken callback and warns once at creation"; the
// warning names the ACP harness id (cursor, github-copilot, grok-build and
// fx forward their setting here).
func TestR91toolsACPMintBridgeTokenIgnored(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	if _, err := CreateACP(testSettings()); err != nil || buf.Len() != 0 {
		t.Fatalf("err = %v, log = %q", err, buf.String())
	}
	minted := false
	h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { minted = true; return "x" } }))
	if err != nil {
		t.Fatal(err)
	}
	if want := "The test-acp harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair.\n"; buf.String() != want {
		t.Fatalf("warning = %q, want %q", buf.String(), want)
	}
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	_ = sess.DoDestroy(context.Background())
	if minted {
		t.Error("mintBridgeToken was called")
	}
}
