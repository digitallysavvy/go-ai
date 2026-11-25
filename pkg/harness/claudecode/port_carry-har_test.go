package claudecode_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS 04a8f3837e (claude-code-harness.test.ts): keepAliveMs reaches both the
// fresh and the attached channel; unset is the 20s default, a negative value
// disables the heartbeat.
func TestPortcarryharKeepAliveForwardedToChannels(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  time.Duration
		want time.Duration
	}{
		{"set", 100 * time.Millisecond, 100 * time.Millisecond},
		{"default", 0, bridge.DefaultKeepAlive},
		{"disabled", -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey})
			t.Cleanup(srv.Close)
			sandbox := newTestSandbox(srv)
			wireSpawn(sandbox)
			h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second, KeepAlive: tc.set})
			if err != nil {
				t.Fatal(err)
			}
			opts := harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"}
			first, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := claudecode.PortcarryharChannelKeepAlive(first); got != tc.want {
				t.Fatalf("fresh channel keepalive = %v, want %v", got, tc.want)
			}
			opts.ResumeFrom, err = first.DoDetach(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			attached, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
			if got := claudecode.PortcarryharChannelKeepAlive(attached); got != tc.want {
				t.Fatalf("attached channel keepalive = %v, want %v", got, tc.want)
			}
		})
	}
}

// killRecorder wraps the test sandbox: it serves a bridge-meta.json with a
// recorded pid, answers the command-line probe with the session's bridge
// state directory, and records every other command with the number of
// spawns seen so far.
type portcarryharKillSandbox struct {
	*testSandbox
	mu       sync.Mutex
	stateDir string
	cmds     []string
	spawnsAt map[string]int
}

func (s *portcarryharKillSandbox) ReadTextFile(ctx context.Context, o providerutils.SandboxReadTextFileOptions) (*string, error) {
	if strings.HasSuffix(o.Path, "/bridge-meta.json") {
		s.mu.Lock()
		s.stateDir = strings.TrimSuffix(o.Path, "/bridge-meta.json")
		s.mu.Unlock()
		if s.spawnsAt != nil && s.testSandbox.SpawnCount() == 0 {
			m := `{"type":"claude-code","state":"waiting","port":4319,"pid":4242}`
			return &m, nil
		}
	}
	return s.testSandbox.ReadTextFile(ctx, o)
}

func (s *portcarryharKillSandbox) Run(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.mu.Lock()
	s.cmds = append(s.cmds, o.Command)
	s.spawnsAt[o.Command] = s.testSandbox.SpawnCount()
	dir := s.stateDir
	s.mu.Unlock()
	switch {
	case strings.HasPrefix(o.Command, "{ tr "):
		return providerutils.SandboxRunResult{Stdout: "node bridge.mjs --bridge-state-dir " + dir}, nil
	case strings.HasPrefix(o.Command, "if [ -r /proc/self/stat ]"):
		if s.testSandbox.SpawnCount() == 0 && !s.killed() {
			return providerutils.SandboxRunResult{Stdout: "4242 1 S t0\n4243 4242 S t1\n"}, nil
		}
		return providerutils.SandboxRunResult{}, nil
	}
	return s.testSandbox.Run(ctx, o)
}

func (s *portcarryharKillSandbox) killed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.cmds {
		if strings.HasPrefix(c, "kill -") {
			return true
		}
	}
	return false
}

func (s *portcarryharKillSandbox) Restricted() providerutils.SandboxSession { return s }

// TS d84bf245a0 'stops the recorded bridge before respawning on a failed
// attach': a rejected attach stops the bridge and its children by the
// recorded pid, and only then spawns the replacement.
func TestPortcarryharFailedAttachStopsRecordedBridgeBeforeRespawn(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey})
	t.Cleanup(srv.Close)
	ts := newTestSandbox(srv)
	wireSpawn(ts)
	sandbox := &portcarryharKillSandbox{testSandbox: ts, spawnsAt: map[string]int{}}

	h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// Coordinates with a key the bridge does not trust: the attach fails.
	other := bridge.CreateBridgeAuthKeyPair()
	resume, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{
		"bridge": map[string]any{"port": 4319, "privateKey": other.PrivateKey, "lastSeenEventId": 0},
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
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	var kill string
	for _, c := range sandbox.cmds {
		if strings.HasPrefix(c, "kill -TERM") {
			kill = c
		}
	}
	if kill != "kill -TERM 4242 4243" {
		t.Fatalf("kill command = %q (commands %q)", kill, sandbox.cmds)
	}
	if n := sandbox.spawnsAt[kill]; n != 0 {
		t.Fatalf("the bridge was stopped after %d spawn(s); it must be stopped before the respawn", n)
	}
	if ts.SpawnCount() != 1 {
		t.Fatalf("SpawnCount = %d, want 1", ts.SpawnCount())
	}
}

// TS 31120c50b0 'preserves session ID from init events': the bridge's reduced
// init raw event carries the session ID; a turn that never finishes still
// records it, so detach puts it into the resume state. An init without a
// usable ID (empty, or another event) records nothing.
func TestPortcarryharInitRawEventRecordsSessionID(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  map[string]any
		want string
	}{
		{"init with id", map[string]any{"type": "system", "subtype": "init", "sessionId": "claude-init-1"}, "claude-init-1"},
		{"empty id", map[string]any{"type": "system", "subtype": "init", "sessionId": ""}, ""},
		{"status event", map[string]any{"type": "system", "subtype": "status", "sessionId": "ignored"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey, OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
				turn.Emit(map[string]any{"type": "stream-start"})
				turn.Emit(map[string]any{"type": "raw", "rawValue": tc.raw})
			}})
			t.Cleanup(srv.Close)
			sandbox := newTestSandbox(srv)
			wireSpawn(sandbox)
			h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
			if err != nil {
				t.Fatal(err)
			}
			sawRaw := make(chan struct{}, 1)
			if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
				Prompt: harness.TextPrompt("hi"),
				Emit: func(p harness.StreamPart) {
					if _, ok := p.(*harness.RawPart); ok {
						select {
						case sawRaw <- struct{}{}:
						default:
						}
					}
				},
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-sawRaw:
			case <-time.After(3 * time.Second):
				t.Fatal("no raw event")
			}
			resume, err := sess.DoDetach(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var data struct {
				ClaudeSessionID string `json:"claudeSessionId"`
			}
			if err := json.Unmarshal(resume.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.ClaudeSessionID != tc.want {
				t.Fatalf("claudeSessionId = %q, want %q", data.ClaudeSessionID, tc.want)
			}
		})
	}
}
