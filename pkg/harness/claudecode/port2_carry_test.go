package claudecode_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS claude-code-harness.test.ts 'leaves the recorded bridge running when
// attaching succeeds': the attach reaches the bridge, so nothing is killed
// and no replacement is spawned. (Companion of
// TestPortcarryharFailedAttachStopsRecordedBridgeBeforeRespawn, which uses
// the same recording sandbox.)
func TestPort2carryAttachSuccessLeavesRecordedBridgeRunning(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey})
	t.Cleanup(srv.Close)
	ts := newTestSandbox(srv)
	wireSpawn(ts)
	sandbox := &portcarryharKillSandbox{testSandbox: ts, spawnsAt: map[string]int{}}

	h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// Coordinates with the key the bridge trusts: the attach succeeds.
	resume, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{
		"bridge": map[string]any{"port": 4319, "privateKey": claudecode.R91carryTestKeys.PrivateKey, "lastSeenEventId": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: resume,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _, _ = sess.DoDetach(context.Background()) })

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	for _, c := range sandbox.cmds {
		if strings.HasPrefix(c, "kill ") {
			t.Fatalf("a successful attach ran %q", c)
		}
	}
	if ts.SpawnCount() != 0 {
		t.Fatalf("SpawnCount = %d, want 0 (the recorded bridge is reused)", ts.SpawnCount())
	}
}
