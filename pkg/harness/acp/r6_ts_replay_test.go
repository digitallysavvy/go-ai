package acp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS acp-harness.test.ts: 'rejects unstable harness id'.
func TestTSReplay_RejectsUnstableHarnessIDs(t *testing.T) {
	for _, id := range []string{"CodexACP", "codex_acp", "codex/acp", "codex--acp", ""} {
		_, err := CreateACP(testSettings(func(s *Settings) { s.HarnessID = id }))
		if err == nil || !strings.Contains(err.Error(), "stable kebab-case identifier") {
			t.Errorf("harnessId %q: err = %v", id, err)
		}
	}
}

// TS: 'requires an exact npm package version'.
func TestTSReplay_RequiresExactNPMVersion(t *testing.T) {
	_, err := CreateACP(testSettings(func(s *Settings) { s.Source.PackageVersion = "^1.1.4" }))
	if err == nil || !strings.Contains(err.Error(), "exact semantic version") {
		t.Fatalf("err = %v", err)
	}
}

// TS: 'resolves the latest dist-tag when no package version is pinned'.
func TestTSReplay_UnpinnedPackageResolvesLatest(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.(harness.BootstrapProvider).GetBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range b.Files {
		if strings.HasSuffix(f.Path, "/implementation/package.json") && strings.Contains(string(f.Content), `"test-acp-agent": "latest"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("implementation/package.json does not pin the latest dist-tag")
	}
}

// TS: 'advertises approvals with and without a complete mapping' and
// 'does not claim native filtering when approval mapping is complete'.
func TestTSReplay_ApprovalAndFilteringCapabilities(t *testing.T) {
	target := &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full"}
	for name, pm := range map[string]*PermissionModeMapping{
		"complete":   {AllowReads: target, AllowEdits: target, AllowAll: target},
		"incomplete": {AllowAll: target, Unset: []harness.PermissionMode{harness.PermissionModeAllowReads, harness.PermissionModeAllowEdits}},
		"none":       nil,
	} {
		h, err := CreateACP(testSettings(func(s *Settings) { s.PermissionModeMapping = pm }))
		if err != nil {
			t.Fatal(err)
		}
		if !harness.SupportsBuiltinToolApprovals(h) || harness.SupportsBuiltinToolFiltering(h) {
			t.Errorf("%s: approvals/filtering = %v/%v", name, harness.SupportsBuiltinToolApprovals(h), harness.SupportsBuiltinToolFiltering(h))
		}
		_, err = h.DoStart(context.Background(), harness.StartOptions{
			SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(newServer(t, "tok", nil)),
			BuiltinToolFiltering: &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{"bash"}},
		})
		var cu *harness.CapabilityUnsupportedError
		if !errors.As(err, &cu) || !strings.Contains(err.Error(), "built-in tool filtering is not available") {
			t.Errorf("%s: filtering err = %v", name, err)
		}
	}
}

// TS: 'supports restrictive permission modes without a mapping' and
// 'transports explicit unsupported permission mode mappings'.
func TestTSReplay_PermissionModeOnStartFrame(t *testing.T) {
	run := func(pm *PermissionModeMapping, mode harness.PermissionMode) map[string]any {
		captured := make(chan map[string]any, 1)
		srv := newServer(t, "tok", func(turn *bridgetest.Turn, start map[string]any) {
			captured <- start
			turn.Emit(finishFrame())
		})
		h, _ := CreateACP(testSettings(func(s *Settings) {
			s.MintBridgeToken = func(string) string { return "tok" }
			s.PermissionModeMapping = pm
		}))
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv), PermissionMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
		c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("p"), Emit: func(harness.StreamPart) {}})
		if err != nil {
			t.Fatal(err)
		}
		<-c.Done()
		return <-captured
	}
	got := run(nil, harness.PermissionModeAllowReads)
	if got["permissionMode"] != "allow-reads" {
		t.Errorf("permissionMode = %v", got["permissionMode"])
	}
	if _, ok := got["permissionModeMapping"]; ok {
		t.Errorf("permissionModeMapping present without a mapping: %v", got["permissionModeMapping"])
	}
	got = run(&PermissionModeMapping{AllowAll: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "agent-full-access"}}, harness.PermissionModeAllowAll)
	pm, _ := got["permissionModeMapping"].(map[string]any)
	if v, ok := pm["allow-reads"]; !ok || v != nil {
		t.Errorf("allow-reads must be an explicit null: %v", pm)
	}
	if v, ok := pm["allow-edits"]; !ok || v != nil {
		t.Errorf("allow-edits must be an explicit null: %v", pm)
	}
	if a, _ := pm["allow-all"].(map[string]any); a["modeId"] != "agent-full-access" || a["type"] != "session-mode" {
		t.Errorf("allow-all = %v", pm["allow-all"])
	}
}

// basicSandbox hides the network surface of a fake sandbox (TS `restricted()`).
type basicSandbox struct{ providerutils.SandboxSession }

func newBasicSandbox(srv *bridgetest.Server) basicSandbox {
	return basicSandbox{newFakeSandbox(srv)}
}

// TS: 'requires explicit bridge settings for a basic sandbox session',
// 'uses a basic sandbox session with explicit bridge settings' and
// 'requires a sandbox id for custom bridge token minting'.
func TestTSReplay_BasicSandboxSession(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) {})
	start := func(mut func(*Settings)) (harness.Session, error) {
		h, err := CreateACP(testSettings(mut))
		if err != nil {
			t.Fatal(err)
		}
		return h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: newBasicSandbox(srv)})
	}
	endpoint := &harness.PortEndpoint{URL: "ws://" + srv.Addr() + "/"}

	_, err := start(func(s *Settings) { s.PortEndpoint = endpoint })
	var cu *harness.CapabilityUnsupportedError
	if !errors.As(err, &cu) || !strings.Contains(err.Error(), "explicit `port`") {
		t.Fatalf("missing port: %v", err)
	}
	_, err = start(func(s *Settings) { s.Port = 4319 })
	if !errors.As(err, &cu) || !strings.Contains(err.Error(), "explicit `portEndpoint`") {
		t.Fatalf("missing portEndpoint: %v", err)
	}
	_, err = start(func(s *Settings) {
		s.Port, s.PortEndpoint = 4319, endpoint
		s.MintBridgeToken = func(id string) string { return id }
	})
	if !errors.As(err, &cu) || !strings.Contains(err.Error(), "does not expose an id") {
		t.Fatalf("mintBridgeToken without id: %v", err)
	}

	sess, err := start(func(s *Settings) {
		s.Port, s.PortEndpoint = 4319, endpoint
		s.MintBridgeToken = nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(state.Data, &data); err != nil {
		t.Fatal(err)
	}
	b, _ := data["bridge"].(map[string]any)
	if b["port"] != float64(4319) || b["token"] == "" || b["lastSeenEventId"] != float64(0) {
		t.Fatalf("bridge coords = %v", b)
	}
	if _, ok := b["sandboxId"]; ok {
		t.Fatalf("basic session must not record a sandboxId: %v", b)
	}
}

func startSession(t *testing.T, srv *bridgetest.Server) harness.Session {
	t.Helper()
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	return sess
}

// TS: 'rejects standard ACP v1 manual compaction without sending a command'.
func TestTSReplay_CompactionUnsupported(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) { t.Error("no frame expected") })
	sess := startSession(t, srv)
	for i := 0; i < 2; i++ {
		err := sess.DoCompact(context.Background(), "")
		var cu *harness.CapabilityUnsupportedError
		if !errors.As(err, &cu) || !strings.Contains(err.Error(), "ACP v1 does not define manual session compaction") {
			t.Fatalf("err = %v", err)
		}
	}
}

// TS: 'keeps stop and destroy idempotent without stopping the framework sandbox'.
func TestTSReplay_StopAndDestroyIdempotent(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) { turn.Emit(finishFrame()) },
		OnFrame: func(ft string) { mu.Lock(); counts[ft]++; mu.Unlock() }})
	t.Cleanup(srv.Close)
	for _, cmd := range []string{"stop", "destroy"} {
		sess := startSession(t, srv)
		c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("p"), Emit: func(harness.StreamPart) {}})
		if err != nil {
			t.Fatal(err)
		}
		<-c.Done()
		if cmd == "stop" {
			first, err := sess.DoStop(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			second, err := sess.DoStop(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(second)
			if string(a) != string(b) {
				t.Fatalf("second stop differs:\n%s\n%s", a, b)
			}
		}
		_ = sess.DoDestroy(context.Background())
		_ = sess.DoDestroy(context.Background())
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if counts["stop"] != 1 || counts["destroy"] != 1 {
		t.Fatalf("frames = %v, want exactly one stop and one destroy", counts)
	}
}

// TS: 'rejects a cancelled turn when the bridge connection fails': partial
// blocks are closed and the turn rejects with the abort reason.
func TestTSReplay_CancelledTurnRejectsWhenConnectionFails(t *testing.T) {
	started := make(chan *bridgetest.Turn, 1)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, _ map[string]any) { started <- turn }})
	t.Cleanup(srv.Close)
	h, _ := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.Reconnect = bridge.ReconnectOptions{MaxElapsed: 300 * time.Millisecond, InitialDelay: 20 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	}))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	ctx, cancel := context.WithCancelCause(context.Background())
	abortErr := errors.New("cancel before disconnect")
	var mu sync.Mutex
	var types []string
	c, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("p"), Emit: func(p harness.StreamPart) {
		mu.Lock()
		types = append(types, string(p.PartType()))
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	turn := <-started
	cancel(abortErr)
	turn.Emit(map[string]any{"type": "reasoning-start", "id": "partial"})
	time.Sleep(50 * time.Millisecond)
	srv.Close() // stop accepting, so every reconnect attempt fails
	srv.DropActive()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not settle after the connection failed")
	}
	if !errors.Is(c.Err(), abortErr) {
		t.Fatalf("Err = %v, want the abort reason", c.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(types) != 2 || types[0] != "reasoning-start" || types[1] != "reasoning-end" {
		t.Fatalf("parts = %v, want [reasoning-start reasoning-end]", types)
	}
}
