package claudecode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func r91carryPrompt(t *testing.T, sess harness.Session) {
	t.Helper()
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Inspect the project."), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
}

// TS 5c3842f9d0 'sends autoMode=%s to the bridge': true and false are sent,
// unset is omitted, next to the permission mode.
func TestR91carryAutoModeSentToBridge(t *testing.T) {
	for _, mode := range []*bool{r85carryBool(true), r85carryBool(false), nil} {
		starts := newR85carryStarts()
		h, sandbox := r85carryHarness(t, claudecode.Settings{AutoMode: mode}, starts)
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", PermissionMode: harness.PermissionModeAllowReads})
		if err != nil {
			t.Fatalf("DoStart: %v", err)
		}
		r91carryPrompt(t, sess)
		start := starts.last(t)
		if start["permissionMode"] != "allow-reads" {
			t.Errorf("permissionMode = %v", start["permissionMode"])
		}
		got, ok := start["autoMode"]
		switch {
		case mode == nil && ok:
			t.Errorf("unset autoMode sent: %v", got)
		case mode != nil && got != *mode:
			t.Errorf("autoMode = %v, want %v", got, *mode)
		}
		_ = sess.DoDestroy(context.Background())
	}
}

// TS 5c3842f9d0 'resumes the exact conversation after detaching and
// attaching': autoMode is on the resumed start (one start-frame builder
// serves fresh, resumed and rerun starts).
func TestR91carryAutoModeOnResume(t *testing.T) {
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{AutoMode: r85carryBool(true)}, starts)
	first, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{"claudeSessionId": "claude-session-1"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: first})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	r91carryPrompt(t, sess)
	start := starts.last(t)
	if start["autoMode"] != true || start["resumeSessionId"] != "claude-session-1" {
		t.Fatalf("resumed start = %v", start)
	}
}

// TS c849d37dac 'ignores a mintBridgeToken callback and warns once at
// creation'; the spawn env carries only the public key and the endpoint no
// token.
func TestR91carryMintBridgeTokenIgnoredWithWarning(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })

	called := 0
	starts := newR85carryStarts()
	h, sandbox := r85carryHarness(t, claudecode.Settings{MintBridgeToken: func(string) string { called++; return "tok" }}, starts)
	var spawnEnv map[string]string
	sandbox.SetSpawn(func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		spawnEnv = opts.Env
		proc := bridgetest.NewProcess()
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
		go func() { time.Sleep(50 * time.Millisecond); proc.Exit(0) }()
		return proc, nil
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	_ = sess.DoDestroy(context.Background())
	if called != 0 {
		t.Fatalf("mintBridgeToken called %d times", called)
	}
	want := "The claude-code harness ignores the deprecated `mintBridgeToken` setting. Sandbox bridge authentication now uses an adapter-generated key pair.\n"
	if buf.String() != want {
		t.Fatalf("warnings = %q, want %q", buf.String(), want)
	}
	if spawnEnv["BRIDGE_AUTH_PUBLIC_KEY"] != claudecode.R91carryTestKeys.PublicKey {
		t.Fatalf("BRIDGE_AUTH_PUBLIC_KEY = %q", spawnEnv["BRIDGE_AUTH_PUBLIC_KEY"])
	}
	if _, ok := spawnEnv["BRIDGE_CHANNEL_TOKEN"]; ok {
		t.Fatal("shared token in the sandbox env")
	}
	if b, _ := json.Marshal(spawnEnv); strings.Contains(string(b), claudecode.R91carryTestKeys.PrivateKey) {
		t.Fatal("private key in the sandbox env")
	}
}

// Lifecycle state (TS claudeCodeBridgeCoordsSchema with privateKey): the
// private key is required; v0.5.2 coordinates with a legacy token stay
// readable but are never attached to, so the bridge is respawned with a
// new key pair (S6, never a token as authentication).
func TestR91carryLifecycleBridgeCoords(t *testing.T) {
	h, err := claudecode.New(claudecode.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data string
		ok   bool
	}{
		{`{"bridge":{"port":1,"privateKey":"k","lastSeenEventId":0}}`, true},
		{`{"bridge":{"port":1,"token":"t","lastSeenEventId":0}}`, true},
		{`{"bridge":{"port":1,"lastSeenEventId":0}}`, false},
		{`{"bridge":{"port":1,"privateKey":null,"token":"t","lastSeenEventId":0}}`, false},
		{`{"bridge":{"privateKey":"k","lastSeenEventId":0}}`, false},
		{`{"bridge":{"port":1,"privateKey":"k"}}`, false},
		{`{"claudeSessionId":"x"}`, true},
	} {
		if err := h.ValidateLifecycleStateData(json.RawMessage(tc.data)); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.data, err, tc.ok)
		}
	}

	starts := newR85carryStarts()
	h2, sandbox := r85carryHarness(t, claudecode.Settings{}, starts)
	sess, err := h2.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy := *resume
	legacy.Data = json.RawMessage(strings.Replace(string(resume.Data), `"privateKey":"`+claudecode.R91carryTestKeys.PrivateKey+`"`, `"token":"legacy-token"`, 1))
	if string(legacy.Data) == string(resume.Data) {
		t.Fatalf("resume data has no privateKey: %s", resume.Data)
	}
	if err := h2.ValidateLifecycleStateData(legacy.Data); err != nil {
		t.Fatalf("legacy state rejected: %v", err)
	}
	again, err := h2.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: &legacy})
	if err != nil {
		t.Fatalf("DoStart from legacy state: %v", err)
	}
	t.Cleanup(func() { _ = again.DoDestroy(context.Background()) })
	if sandbox.SpawnCount() != 2 {
		t.Fatalf("spawns = %d, want 2 (legacy token coordinates are not attached to)", sandbox.SpawnCount())
	}
}
