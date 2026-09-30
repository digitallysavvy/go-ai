package bridge_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Port of harness/src/utils/bridge-ready.test.ts. This file lives in the
// bridge_test package (not bridge) because bridgetest imports bridge, and an
// in-package test importing bridgetest would create an import cycle.

// TS: "resolves from stdout bridge-ready"
func TestWaitForBridgeReadyResolvesFromStdout(t *testing.T) {
	proc := bridgetest.NewProcess()
	proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
	sandbox := bridgetest.NewSandbox()

	result, err := bridge.WaitForBridgeReady(context.Background(), bridge.WaitForBridgeReadyOptions{
		Proc:           proc,
		Sandbox:        sandbox,
		BridgeStateDir: "/state",
		BridgeType:     "claude-code",
		Timeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("WaitForBridgeReady: %v", err)
	}
	if result.Port != 4319 || result.Source != bridge.ReadySourceStdout {
		t.Fatalf("got %+v, want port 4319 source stdout", result)
	}
}

// TS: "resolves from bridge metadata when stdout does not deliver"
func TestWaitForBridgeReadyResolvesFromMetadataFallback(t *testing.T) {
	proc := bridgetest.NewProcess() // stdout never writes anything (pending)
	sandbox := bridgetest.NewSandbox()
	meta, _ := json.Marshal(map[string]any{
		"type": "claude-code", "port": 4319, "state": "waiting", "pid": 123,
	})
	sandbox.SetFile(bridge.BridgeMetaPath("/state"), string(meta))

	result, err := bridge.WaitForBridgeReady(context.Background(), bridge.WaitForBridgeReadyOptions{
		Proc:           proc,
		Sandbox:        sandbox,
		BridgeStateDir: "/state",
		BridgeType:     "claude-code",
		Timeout:        time.Second,
		PollInterval:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("WaitForBridgeReady: %v", err)
	}
	if result.Port != 4319 || result.Source != bridge.ReadySourceMetadata {
		t.Fatalf("got %+v, want port 4319 source metadata", result)
	}
}

// sequencedSandbox serves a fixed sequence of ReadTextFile results in order,
// counting calls (TS `vi.fn(async () => reads.shift() ?? null)`).
type sequencedSandbox struct {
	*bridgetest.Sandbox
	mu    sync.Mutex
	reads []string
	calls int32
}

func (s *sequencedSandbox) ReadTextFile(context.Context, providerutils.SandboxReadTextFileOptions) (*string, error) {
	atomic.AddInt32(&s.calls, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reads) == 0 {
		return nil, nil
	}
	v := s.reads[0]
	s.reads = s.reads[1:]
	return &v, nil
}

func (s *sequencedSandbox) callCount() int { return int(atomic.LoadInt32(&s.calls)) }

// TS: "ignores non-waiting metadata"
func TestWaitForBridgeReadyIgnoresNonWaitingMetadata(t *testing.T) {
	proc := bridgetest.NewProcess()
	starting, _ := json.Marshal(map[string]any{"type": "claude-code", "state": "starting"})
	waiting, _ := json.Marshal(map[string]any{"type": "claude-code", "port": 4319, "state": "waiting"})
	sandbox := &sequencedSandbox{Sandbox: bridgetest.NewSandbox(), reads: []string{string(starting), string(waiting)}}

	result, err := bridge.WaitForBridgeReady(context.Background(), bridge.WaitForBridgeReadyOptions{
		Proc:           proc,
		Sandbox:        sandbox,
		BridgeStateDir: "/state",
		BridgeType:     "claude-code",
		Timeout:        time.Second,
		PollInterval:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("WaitForBridgeReady: %v", err)
	}
	if result.Port != 4319 || result.Source != bridge.ReadySourceMetadata {
		t.Fatalf("got %+v, want port 4319 source metadata", result)
	}
	if got := sandbox.callCount(); got != 2 {
		t.Fatalf("ReadTextFile called %d times, want 2", got)
	}
}

// TS: "waits for the poll interval between metadata reads" (fake timers in
// TS; here we use real timers with a generous poll interval and check call
// counts before/after it elapses).
func TestWaitForBridgeReadyWaitsForPollInterval(t *testing.T) {
	proc := bridgetest.NewProcess()
	starting, _ := json.Marshal(map[string]any{"type": "claude-code", "state": "starting"})
	waiting, _ := json.Marshal(map[string]any{"type": "claude-code", "port": 4319, "state": "waiting"})
	sandbox := &sequencedSandbox{Sandbox: bridgetest.NewSandbox(), reads: []string{string(starting), string(waiting)}}

	resultCh := make(chan bridge.ReadyResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := bridge.WaitForBridgeReady(context.Background(), bridge.WaitForBridgeReadyOptions{
			Proc:           proc,
			Sandbox:        sandbox,
			BridgeStateDir: "/state",
			BridgeType:     "claude-code",
			Timeout:        2 * time.Second,
			PollInterval:   150 * time.Millisecond,
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	// Wait for the first (starting) read, then check that no second read has
	// happened yet: the 150ms poll interval starts after the first read, so
	// the second cannot follow immediately. Polling for the first read
	// (instead of a fixed sleep) keeps this stable when the goroutine is
	// scheduled late under load.
	waitForT(t, time.Second, func() bool { return sandbox.callCount() >= 1 })
	if got := sandbox.callCount(); got != 1 {
		t.Fatalf("right after the first read: ReadTextFile called %d times, want 1", got)
	}

	select {
	case err := <-errCh:
		t.Fatalf("WaitForBridgeReady: %v", err)
	case result := <-resultCh:
		if result.Port != 4319 || result.Source != bridge.ReadySourceMetadata {
			t.Fatalf("got %+v, want port 4319 source metadata", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForBridgeReady did not resolve in time")
	}
	if got := sandbox.callCount(); got != 2 {
		t.Fatalf("ReadTextFile called %d times, want 2", got)
	}
}

// TS: "marks bridge startup metadata"
func TestMarkBridgeStarting(t *testing.T) {
	sandbox := bridgetest.NewSandbox()
	bridge.MarkBridgeStarting(context.Background(), sandbox, "/state", "codex")

	content, ok := sandbox.File("/state/bridge-meta.json")
	if !ok {
		t.Fatal("bridge-meta.json was not written")
	}
	want, _ := json.Marshal(struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}{"codex", "starting"})
	if content != string(want) {
		t.Fatalf("content = %s, want %s", content, want)
	}
}
