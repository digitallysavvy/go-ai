package bridge

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports of harness/src/utils/stop-previous-bridge.test.ts (TS d84bf245a0)
// on a scripted sandbox: pids live in a table, `kill` marks them dead.

type portcarryharProc struct {
	ppid  int
	start string
	state string
	// survivesTerm: ignores SIGTERM.
	survivesTerm bool
	dead         bool
}

type portcarryharSandbox struct {
	providerutils.SandboxSession
	mu      sync.Mutex
	meta    *string
	cmdline map[int]string
	procs   map[int]*portcarryharProc
	cmds    []string
}

func (s *portcarryharSandbox) ReadTextFile(context.Context, providerutils.SandboxReadTextFileOptions) (*string, error) {
	return s.meta, nil
}

func (s *portcarryharSandbox) Run(_ context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cmds = append(s.cmds, o.Command)
	switch {
	case strings.HasPrefix(o.Command, "{ tr "):
		for pid, c := range s.cmdline {
			if strings.Contains(o.Command, "/proc/"+itoa(pid)+"/cmdline") {
				return providerutils.SandboxRunResult{Stdout: c}, nil
			}
		}
		return providerutils.SandboxRunResult{}, nil
	case strings.HasPrefix(o.Command, "if [ -r /proc/self/stat ]"):
		var b strings.Builder
		for pid, p := range s.procs {
			if p.dead {
				continue
			}
			state := p.state
			if state == "" {
				state = "S"
			}
			b.WriteString(itoa(pid) + " " + itoa(p.ppid) + " " + state + " " + p.start + "\n")
		}
		return providerutils.SandboxRunResult{Stdout: b.String()}, nil
	case strings.HasPrefix(o.Command, "kill -"):
		fields := strings.Fields(o.Command)
		sig := strings.TrimPrefix(fields[1], "-")
		for _, f := range fields[2:] {
			if p := s.procs[atoi(f)]; p != nil && (sig == "KILL" || !p.survivesTerm) {
				p.dead = true
			}
		}
	}
	return providerutils.SandboxRunResult{}, nil
}

func itoa(i int) string { return fmtInt(i) }
func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}
func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func (s *portcarryharSandbox) killed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.cmds {
		if strings.HasPrefix(c, "kill -") {
			out = append(out, c)
		}
	}
	return out
}

func portcarryharMeta(s string) *string { return &s }

const portcarryharStateDir = "/home/a/.state/bridge"

func newPortcarryharSandbox() *portcarryharSandbox {
	return &portcarryharSandbox{
		meta:    portcarryharMeta(`{"type":"claude-code","state":"waiting","port":4319,"pid":100}`),
		cmdline: map[int]string{100: "node bridge.mjs --bridge-state-dir " + portcarryharStateDir + " "},
		procs: map[int]*portcarryharProc{
			100: {ppid: 1, start: "t100"},
			101: {ppid: 100, start: "t101"},
			102: {ppid: 101, start: "t102"},
			200: {ppid: 1, start: "t200"},
		},
	}
}

func portcarryharStop(s *portcarryharSandbox, grace time.Duration) error {
	return StopPreviousBridge(context.Background(), StopPreviousBridgeOptions{
		Sandbox: s, BridgeStateDir: portcarryharStateDir, BridgeType: "claude-code", GracePeriod: grace,
	})
}

// TERM goes to the bridge first and to every descendant, nothing else.
func TestPortcarryharStopPreviousBridgeSignalsTree(t *testing.T) {
	s := newPortcarryharSandbox()
	if err := portcarryharStop(s, time.Second); err != nil {
		t.Fatal(err)
	}
	if got := s.killed(); len(got) != 1 || got[0] != "kill -TERM 100 101 102" {
		t.Fatalf("kill commands = %q", got)
	}
	if s.procs[200].dead {
		t.Fatal("an unrelated process was signalled")
	}
}

// A process that survives TERM gets KILL after the grace period.
func TestPortcarryharStopPreviousBridgeEscalatesToKill(t *testing.T) {
	s := newPortcarryharSandbox()
	s.procs[102].survivesTerm = true
	if err := portcarryharStop(s, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	got := s.killed()
	if len(got) != 2 || got[0] != "kill -TERM 100 101 102" || got[1] != "kill -KILL 102" {
		t.Fatalf("kill commands = %q", got)
	}
}

// Nothing is signalled without a usable recorded pid, for a stale pid whose
// command line is not this bridge, or for a bridge of another type.
func TestPortcarryharStopPreviousBridgeLeavesOthersAlone(t *testing.T) {
	for name, mutate := range map[string]func(*portcarryharSandbox){
		"no meta":        func(s *portcarryharSandbox) { s.meta = nil },
		"no pid":         func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"claude-code"}`) },
		"pid zero":       func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"claude-code","pid":0}`) },
		"negative pid":   func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"claude-code","pid":-1}`) },
		"fractional pid": func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"claude-code","pid":1.5}`) },
		"null pid":       func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"claude-code","pid":null}`) },
		"other type":     func(s *portcarryharSandbox) { s.meta = portcarryharMeta(`{"type":"codex","pid":100}`) },
		"stale pid":      func(s *portcarryharSandbox) { s.cmdline[100] = "node something-else.js" },
	} {
		t.Run(name, func(t *testing.T) {
			s := newPortcarryharSandbox()
			mutate(s)
			if err := portcarryharStop(s, 50*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if got := s.killed(); len(got) != 0 {
				t.Fatalf("signalled %q", got)
			}
		})
	}
}

// A pid reused by another process (different start time) is dropped, not
// signalled again; a zombie is skipped.
func TestPortcarryharStopPreviousBridgeSkipsReusedPidAndZombie(t *testing.T) {
	s := newPortcarryharSandbox()
	s.procs[101].state = "Z"
	if err := portcarryharStop(s, time.Second); err != nil {
		t.Fatal(err)
	}
	if got := s.killed(); len(got) != 1 || got[0] != "kill -TERM 100" {
		t.Fatalf("kill commands = %q (a zombie has exited and cannot be signalled)", got)
	}
}

func TestPortcarryharStopPreviousBridgeAbort(t *testing.T) {
	s := newPortcarryharSandbox()
	s.procs[100].survivesTerm = true
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel(context.DeadlineExceeded) }()
	err := StopPreviousBridge(ctx, StopPreviousBridgeOptions{
		Sandbox: s, BridgeStateDir: portcarryharStateDir, BridgeType: "claude-code", GracePeriod: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("want the abort error")
	}
}

// ReadBridgeMeta: zod rejects a null or mistyped key.
func TestPortcarryharReadBridgeMetaSchema(t *testing.T) {
	for raw, wantNil := range map[string]bool{
		`{"type":"x","pid":7}`:        false,
		`{"type":"x","port":"4"}`:     true,
		`{"type":"x","state":null}`:   true,
		`{"type":"y","pid":7}`:        true,
		`{"pid":7}`:                   true,
		`{"type":"x","port":1,"a":2}`: false,
	} {
		s := &portcarryharSandbox{meta: portcarryharMeta(raw)}
		got := ReadBridgeMeta(context.Background(), s, "/d", "x")
		if (got == nil) != wantNil {
			t.Errorf("%s: got %+v, wantNil %v", raw, got, wantNil)
		}
	}
}
