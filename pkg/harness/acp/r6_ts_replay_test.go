package acp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"reflect"
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

// TS: 'requires an exact npm package version' (deprecated npm-simple shim).
func TestTSReplay_RequiresExactNPMVersion(t *testing.T) {
	_, err := CreateACP(testSettings(func(s *Settings) {
		s.Source = Source{Type: SourceNPMSimple, PackageName: "test-acp-agent", PackageVersion: "^1.1.4"}
	}))
	if err == nil || !strings.Contains(err.Error(), "exact semantic version") {
		t.Fatalf("err = %v", err)
	}
}

// TS: 'resolves the latest dist-tag when no package version is pinned'.
func TestTSReplay_UnpinnedPackageResolvesLatest(t *testing.T) {
	h, err := CreateACP(testSettings(func(s *Settings) { s.Source = Source{Type: SourceNPMSimple, PackageName: "test-acp-agent"} }))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.(harness.BootstrapProvider).GetBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range b.Files {
		if strings.HasSuffix(f.Path, "/install.sh") && strings.Contains(f.Content, "'test-acp-agent@latest'") {
			found = true
		}
	}
	if !found {
		t.Fatal("install.sh does not install the latest dist-tag")
	}
}

// TS: 'advertises approvals with and without a complete mapping' and
// 'does not claim native filtering when approval mapping is complete'.
func TestTSReplay_ApprovalAndFilteringCapabilities(t *testing.T) {
	target := &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full"}
	for name, pm := range map[string]*PermissionModeMapping{
		"complete":   {AllowReads: target, AllowEdits: target, AllowAll: target},
		"incomplete": {AllowAll: target, UnsetAllowReads: true, UnsetAllowEdits: true},
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
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
	t.Cleanup(srv.Close)
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

func partTypeRecorder() (func(harness.StreamPart), func() []string) {
	var mu sync.Mutex
	var types []string
	return func(p harness.StreamPart) {
			mu.Lock()
			types = append(types, string(p.PartType()))
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), types...)
		}
}

func waitForParts(t *testing.T, get func() []string, n int) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if len(get()) >= n {
			return
		}
	}
	t.Fatalf("timed out waiting for %d parts, have %v", n, get())
}

// TS: 'suspends at an exact cursor and resumes a nested continuation by
// replay only' and 'continues a terminal event replayed before the
// continuation is wired'.
func TestTSReplay_SuspendAndAttachContinuation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		beforeSuspend []map[string]any
		wantFirst     []string
		afterAttach   []map[string]any
		wantReplayed  []string
	}{
		{"nested continuation by replay", []map[string]any{
			{"type": "bridge-thread", "threadId": "acp-session-1"}, {"type": "stream-start"},
			{"type": "text-start", "id": "text-1"}, {"type": "text-delta", "id": "text-1", "delta": "before"},
		}, []string{"stream-start", "text-start", "text-delta"}, []map[string]any{
			{"type": "text-delta", "id": "text-1", "delta": " after"}, {"type": "text-end", "id": "text-1"}, finishFrame(),
		}, []string{"text-delta", "text-end", "finish"}},
		{"terminal event replayed before wiring", []map[string]any{{"type": "bridge-thread", "threadId": "acp-session-1"}}, nil,
			[]map[string]any{finishFrame()}, []string{"finish"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var starts, resumes int
			started := make(chan *bridgetest.Turn, 2)
			srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
				OnStart: func(turn *bridgetest.Turn, _ map[string]any) { started <- turn },
				OnFrame: func(ft string) {
					mu.Lock()
					defer mu.Unlock()
					switch ft {
					case "start":
						starts++
					case "resume":
						resumes++
					}
				}})
			t.Cleanup(srv.Close)
			sandbox := newFakeSandbox(srv)
			h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
			ctx := context.Background()
			first, err := h.DoStart(ctx, harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox})
			if err != nil {
				t.Fatal(err)
			}
			emit, delivered := partTypeRecorder()
			turn1, err := first.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Work for a while."), Emit: emit})
			if err != nil {
				t.Fatal(err)
			}
			bt := <-started
			for _, f := range tc.beforeSuspend {
				bt.Emit(f)
			}
			waitForParts(t, delivered, len(tc.wantFirst))
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
				ss := first.(*session)
				ss.mu.Lock()
				id := ss.latestACPSessionID
				ss.mu.Unlock()
				if id != "" {
					break
				}
			}
			continueFrom, err := first.DoSuspendTurn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			<-turn1.Done()
			if got := delivered(); len(got) != len(tc.wantFirst) || (len(got) > 0 && !reflect.DeepEqual(got, tc.wantFirst)) {
				t.Fatalf("delivered = %v, want %v", got, tc.wantFirst)
			}
			var data map[string]any
			if err := json.Unmarshal(continueFrom.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data["acpSessionId"] != "acp-session-1" || data["initialGuidanceApplied"] != true {
				t.Fatalf("lifecycle data = %v", data)
			}

			if tc.name != "nested continuation by replay" {
				// The terminal event is already in the bridge log before the
				// continuation attaches.
				bt.Emit(finishFrame())
			}
			resumed, err := h.DoStart(ctx, harness.StartOptions{
				SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox,
				ResumeFrom: &harness.ResumeSessionState{Type: "resume-session", HarnessID: "test-acp", SpecificationVersion: harness.SpecificationVersion, Data: continueFrom.Data, ContinueFrom: continueFrom},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = resumed.DoDestroy(ctx) })
			sandbox.mu.Lock()
			spawns := len(sandbox.spawnCommands)
			sandbox.mu.Unlock()
			if spawns != 1 {
				t.Fatalf("spawns = %d, want 1 (attach, no respawn)", spawns)
			}
			emit2, replayed := partTypeRecorder()
			cont, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{Emit: emit2})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			if starts != 1 {
				t.Errorf("start frames = %d, want 1 (continuation must not send start)", starts)
			}
			mu.Unlock()
			if tc.name == "nested continuation by replay" {
				for _, f := range tc.afterAttach {
					bt.Emit(f)
				}
			}
			select {
			case <-cont.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("continuation did not finish")
			}
			if got := replayed(); !reflect.DeepEqual(got, tc.wantReplayed) {
				t.Fatalf("replayed = %v, want %v", got, tc.wantReplayed)
			}
		})
	}
}
