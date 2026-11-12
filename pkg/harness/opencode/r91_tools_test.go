package opencode

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

// r91toolsCaptureLog redirects the standard logger (TS console.warn) for one
// test.
func r91toolsCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	return &buf
}

// TS c849d37dac opencode-harness.test.ts: public-key bridge authentication.
// The sandbox gets only BRIDGE_AUTH_PUBLIC_KEY, resume state carries
// bridge.privateKey, the private key never reaches the sandbox, an attach
// reuses it, and legacy token state is rejected.
func TestR91toolsOpenCodeBridgePublicKeyAuth(t *testing.T) {
	key := bridgeauthkeys.Testing()
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h := mustOC(t, Settings{})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	sandbox.mu.Lock()
	env, commands := sandbox.spawnEnvs[0], append([]string(nil), sandbox.spawnCommands...)
	sandbox.mu.Unlock()
	if env[bridge.EnvAuthPublicKey] != key.PublicKey {
		t.Errorf("BRIDGE_AUTH_PUBLIC_KEY = %q", env[bridge.EnvAuthPublicKey])
	}
	if _, ok := env["BRIDGE_CHANNEL_TOKEN"]; ok {
		t.Error("spawn env carries BRIDGE_CHANNEL_TOKEN")
	}
	all, _ := json.Marshal(map[string]any{"env": env, "commands": commands, "files": sandbox.files})
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
	if err := h.(*openCodeHarness).ValidateLifecycleStateData(resume.Data); err != nil {
		t.Errorf("own resume state rejected: %v", err)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox, ResumeFrom: resume})
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
	if err := h.(*openCodeHarness).ValidateLifecycleStateData(json.RawMessage(`{"bridge":{"port":4319,"token":"legacy-token","lastSeenEventId":0}}`)); err == nil {
		t.Fatal("legacy token state accepted")
	}
}

// TS "ignores a mintBridgeToken callback and warns once at creation".
func TestR91toolsOpenCodeMintBridgeTokenIgnored(t *testing.T) {
	buf := r91toolsCaptureLog(t)
	mustOC(t, Settings{})
	if buf.Len() != 0 {
		t.Fatalf("warning without mintBridgeToken: %q", buf.String())
	}
	minted := false
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	h := mustOC(t, Settings{MintBridgeToken: func(string) string { minted = true; return "x" }})
	if want := "The opencode harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair.\n"; buf.String() != want {
		t.Fatalf("warning = %q, want %q", buf.String(), want)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	_ = sess.DoDestroy(context.Background())
	if minted {
		t.Error("mintBridgeToken was called")
	}
}
