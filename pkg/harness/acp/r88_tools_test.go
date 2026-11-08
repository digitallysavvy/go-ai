package acp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS deeb2bfb62 acp-harness.test.ts "awaits sandbox preparation after
// authentication files and before bridge startup": PrepareSandbox gets the
// restricted sandbox and the work dir after the authentication files are
// written and before the bridge is spawned; its error rejects the start.
func TestR88toolsPrepareSandboxRunsAfterAuthFilesBeforeSpawn(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	failure := errors.New("preparation failed")
	var mu sync.Mutex
	var got PrepareSandboxOptions
	var authWritten bool
	var spawnsBefore = -1
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.AuthenticationFiles = func(map[string]string, map[string]string, bool) []AuthenticationFile {
			return []AuthenticationFile{{Path: ".agent/auth.json", Content: "{}"}}
		}
		s.PrepareSandbox = func(_ context.Context, opts PrepareSandboxOptions) error {
			mu.Lock()
			defer mu.Unlock()
			got = opts
			sandbox.mu.Lock()
			for path := range sandbox.files {
				if strings.HasSuffix(path, "/.agent/auth.json") {
					authWritten = true
				}
			}
			spawnsBefore = len(sandbox.spawnCommands)
			sandbox.mu.Unlock()
			return failure
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/workspace/project", SandboxSession: sandbox,
	})
	if !errors.Is(err, failure) {
		t.Fatalf("DoStart err = %v, want the preparation error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.WorkDir != "/workspace/project" || got.Sandbox == nil {
		t.Fatalf("options = %+v", got)
	}
	if !authWritten || spawnsBefore != 0 {
		t.Fatalf("auth written before preparation %v, spawns before %d", authWritten, spawnsBefore)
	}
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) != 0 {
		t.Fatalf("bridge spawned after a failed preparation: %v", sandbox.spawnCommands)
	}
}

// TS deeb2bfb62 "supports synchronous preparation and skips it when
// reattaching" (and a cancelled start never reaches it).
func TestR88toolsPrepareSandboxSkippedOnReattach(t *testing.T) {
	const token = "r88-prepare-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	var mu sync.Mutex
	calls := 0
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.PrepareSandbox = func(context.Context, PrepareSandboxOptions) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/workspace/p", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/workspace/p", SandboxSession: sandbox, ResumeFrom: resumeFrom})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("PrepareSandbox calls = %d, want 1 (skipped on reattach)", calls)
	}
}

// TS a54f0fd8c6 acp-harness.test.ts: the flag follows --bridge-type.
func TestR88toolsACPHostDisconnectTimeout(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		want    string
	}{{60 * time.Second, "--bridge-type 'test-acp' --host-disconnect-timeout-ms '60000'"}, {0, ""}} {
		srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
		t.Cleanup(srv.Close)
		sandbox := newFakeSandbox(srv)
		h, err := CreateACP(testSettings(func(s *Settings) { s.HostDisconnectTimeout = tc.timeout }))
		if err != nil {
			t.Fatal(err)
		}
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/workspace/p", SandboxSession: sandbox})
		if err == nil {
			_ = sess.DoDestroy(context.Background())
		}
		sandbox.mu.Lock()
		if len(sandbox.spawnCommands) == 0 {
			sandbox.mu.Unlock()
			t.Fatalf("no spawn (DoStart: %v)", err)
		}
		cmd := sandbox.spawnCommands[0]
		sandbox.mu.Unlock()
		if tc.want != "" && !strings.HasSuffix(cmd, tc.want) {
			t.Fatalf("command = %q", cmd)
		}
		if tc.want == "" && strings.Contains(cmd, "host-disconnect") {
			t.Fatalf("command = %q", cmd)
		}
	}
}
