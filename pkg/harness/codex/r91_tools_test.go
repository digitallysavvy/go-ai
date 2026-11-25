package codex_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/bridgeauthkeys"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS c849d37dac codex-harness.test.ts: the sandbox gets only the public key
// (BRIDGE_AUTH_PUBLIC_KEY, no BRIDGE_CHANNEL_TOKEN), the host answers the
// bridge's challenge with the private key, resume state carries
// bridge.privateKey (never bridge.token), the private key never reaches the
// sandbox, and an attach reuses the persisted key.
func TestR91toolsCodexBridgePublicKeyAuth(t *testing.T) {
	key := bridgeauthkeys.Testing()
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: key.PublicKey})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	var mu sync.Mutex
	var spawns []providerutils.SandboxProcessOptions
	sandbox.SetSpawn(func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		mu.Lock()
		spawns = append(spawns, opts)
		mu.Unlock()
		proc := bridgetest.NewProcess()
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
		go func() { time.Sleep(20 * time.Millisecond); proc.Exit(0) }()
		return proc, nil
	})
	h := codex.New(codex.Settings{StartupTimeout: 2 * time.Second})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	mu.Lock()
	if len(spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(spawns))
	}
	env, command := spawns[0].Env, spawns[0].Command
	mu.Unlock()
	if env[bridge.EnvAuthPublicKey] != key.PublicKey {
		t.Errorf("BRIDGE_AUTH_PUBLIC_KEY = %q, want the session public key", env[bridge.EnvAuthPublicKey])
	}
	if _, ok := env["BRIDGE_CHANNEL_TOKEN"]; ok {
		t.Error("spawn env carries BRIDGE_CHANNEL_TOKEN")
	}
	all, _ := json.Marshal(map[string]any{"env": env, "command": command})
	if strings.Contains(string(all), key.PrivateKey) {
		t.Fatal("the bridge private key reached the sandbox")
	}
	resume, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	var data struct {
		Bridge map[string]any `json:"bridge"`
	}
	if err := json.Unmarshal(resume.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bridge["privateKey"] != key.PrivateKey {
		t.Errorf("bridge.privateKey = %v", data.Bridge["privateKey"])
	}
	if _, ok := data.Bridge["token"]; ok {
		t.Error("resume state carries bridge.token")
	}
	if err := h.ValidateLifecycleStateData(resume.Data); err != nil {
		t.Errorf("own resume state rejected: %v", err)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: resume})
	if err != nil {
		t.Fatalf("DoStart (attach): %v", err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	mu.Lock()
	n := len(spawns)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("attach spawned %d bridges, want none", n-1)
	}
}

// TS "rejects lifecycle state with a legacy bridge token".
func TestR91toolsCodexRejectsLegacyTokenState(t *testing.T) {
	h := codex.New(codex.Settings{})
	if err := h.ValidateLifecycleStateData(json.RawMessage(`{"bridge":{"port":4319,"token":"legacy-token","lastSeenEventId":0}}`)); err == nil {
		t.Fatal("legacy token state accepted")
	}
}

// TS "ignores a mintBridgeToken callback and warns once at creation".
func TestR91toolsCodexMintBridgeTokenIgnored(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	_ = codex.New(codex.Settings{})
	if buf.Len() != 0 {
		t.Fatalf("warning without mintBridgeToken: %q", buf.String())
	}
	minted := false
	key := bridgeauthkeys.Testing()
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: key.PublicKey})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h := codex.New(codex.Settings{StartupTimeout: 2 * time.Second, MintBridgeToken: func(string) string { minted = true; return "x" }})
	want := "The codex harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair.\n"
	if buf.String() != want {
		t.Fatalf("warning = %q, want %q", buf.String(), want)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	_ = sess.DoDestroy(context.Background())
	if minted {
		t.Error("mintBridgeToken was called")
	}
	if strings.Count(buf.String(), "mintBridgeToken") != 1 {
		t.Errorf("warned %d times, want once", strings.Count(buf.String(), "mintBridgeToken"))
	}
}
