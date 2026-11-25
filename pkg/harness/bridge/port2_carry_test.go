//go:build unix

package bridge

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Twins of stop-previous-bridge.test.ts 'stopPreviousBridge with real
// processes': stand-in bridge processes (this test binary re-executed) with
// a real shell as the sandbox.

const port2carryHelperEnv = "PORT2CARRY_BRIDGE_HELPER"

// init turns the re-executed test binary into the stand-in process when the
// helper mode is set; a normal test run is unaffected.
func init() {
	if mode := os.Getenv(port2carryHelperEnv); mode != "" {
		port2carryHelperMain(mode)
	}
}

func port2carryHelperMain(mode string) {
	dir := os.Getenv("PORT2CARRY_DIR")
	onTerm := func(marker string) {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		go func() {
			<-c
			_ = os.WriteFile(marker, nil, 0o600)
			os.Exit(0)
		}()
	}
	spawn := func(childMode string) *bufio.Reader {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), port2carryHelperEnv+"="+childMode)
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		return bufio.NewReader(out)
	}
	switch mode {
	case "tool":
		onTerm(filepath.Join(dir, "tool-stopped"))
		_, _ = os.Stdout.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	case "runtime":
		onTerm(filepath.Join(dir, "runtime-stopped"))
		line, _ := spawn("tool").ReadString('\n')
		_, _ = os.Stdout.WriteString(strconv.Itoa(os.Getpid()) + " " + strings.TrimSpace(line) + "\n")
	case "bridge":
		line, _ := spawn("runtime").ReadString('\n')
		_, _ = os.Stdout.WriteString(strings.TrimSpace(line) + "\n")
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		_, _ = os.Stdout.WriteString("ready\n")
	case "plain":
		_, _ = os.Stdout.WriteString("ready\n")
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

type port2carryLocalSandbox struct{ providerutils.SandboxSession }

func (port2carryLocalSandbox) Run(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", o.Command)
	out, err := cmd.Output()
	res := providerutils.SandboxRunResult{Stdout: string(out)}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	return res, err
}

func (port2carryLocalSandbox) ReadTextFile(_ context.Context, o providerutils.SandboxReadTextFileOptions) (*string, error) {
	b, err := os.ReadFile(o.Path)
	if err != nil {
		return nil, nil
	}
	s := string(b)
	return &s, nil
}

type port2carryProc struct {
	cmd       *exec.Cmd
	dir       string
	firstLine string
	exited    chan syscall.Signal
}

func port2carryStart(t *testing.T, mode string, namesStateDir bool) *port2carryProc {
	t.Helper()
	dir := t.TempDir()
	var args []string
	if namesStateDir {
		args = append(args, "--bridge-state-dir", dir)
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), port2carryHelperEnv+"="+mode, "PORT2CARRY_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &port2carryProc{cmd: cmd, dir: dir, exited: make(chan syscall.Signal, 1)}
	line, _ := bufio.NewReader(out).ReadString('\n')
	p.firstLine = strings.TrimSpace(line)
	go func() {
		_ = cmd.Wait()
		var sig syscall.Signal
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			sig = ws.Signal()
		}
		p.exited <- sig
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		for _, f := range strings.Fields(p.firstLine) {
			if pid, err := strconv.Atoi(f); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	meta := `{"type":"claude-code","pid":` + strconv.Itoa(cmd.Process.Pid) + `}`
	if err := os.WriteFile(filepath.Join(dir, "bridge-meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func port2carryStop(p *port2carryProc, grace time.Duration) error {
	return StopPreviousBridge(context.Background(), StopPreviousBridgeOptions{
		Sandbox: port2carryLocalSandbox{}, BridgeStateDir: p.dir, BridgeType: "claude-code", GracePeriod: grace,
	})
}

func (p *port2carryProc) waitExit(t *testing.T) syscall.Signal {
	t.Helper()
	select {
	case s := <-p.exited:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("the process did not exit")
		return 0
	}
}

// TS 'stops the bridge and every process it started'.
func TestPort2carryStopPreviousBridgeRealProcessTree(t *testing.T) {
	p := port2carryStart(t, "bridge", true)
	if err := port2carryStop(p, 0); err != nil {
		t.Fatal(err)
	}
	if sig := p.waitExit(t); sig != syscall.SIGTERM {
		t.Fatalf("bridge exit signal = %v, want SIGTERM", sig)
	}
	for _, marker := range []string{"runtime-stopped", "tool-stopped"} {
		if _, err := os.Stat(filepath.Join(p.dir, marker)); err != nil {
			t.Fatalf("%s: %v (every process must be signalled directly)", marker, err)
		}
	}
}

// TS 'kills a bridge that ignores SIGTERM'.
func TestPort2carryStopPreviousBridgeRealProcessKill(t *testing.T) {
	p := port2carryStart(t, "ignore-term", true)
	if err := port2carryStop(p, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig := p.waitExit(t); sig != syscall.SIGKILL {
		t.Fatalf("bridge exit signal = %v, want SIGKILL", sig)
	}
}

// TS 'leaves a recorded process alone when it does not run the bridge'.
func TestPort2carryStopPreviousBridgeRealProcessOther(t *testing.T) {
	p := port2carryStart(t, "plain", false)
	// The recorded process does not name the state directory.
	if err := port2carryStop(p, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-p.exited:
		t.Fatalf("the process was stopped (signal %v)", sig)
	case <-time.After(500 * time.Millisecond):
	}
}
