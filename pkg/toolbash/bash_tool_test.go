//go:build unix

package toolbash

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func intPtr(v int) *int { return &v }

// newSandbox returns a restricted local sandbox session (the TS tests use
// sandbox-just-bash's restricted() session) rooted in a temp dir.
func newSandbox(t *testing.T) (providerutils.SandboxSession, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := local.NewProvider(local.Options{RootDir: root, HostEnv: []string{"PATH"}})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{SessionID: t.Name()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Destroy(context.Background()) })
	return session.Restricted(), session.DefaultWorkingDirectory()
}

// recordingSandbox records the calls the tool makes.
type recordingSandbox struct {
	providerutils.SandboxSession
	mu     sync.Mutex
	runs   []providerutils.SandboxProcessOptions
	spawns []providerutils.SandboxProcessOptions
	runFn  func(providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error)
}

func (r *recordingSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	r.mu.Lock()
	r.runs = append(r.runs, opts)
	fn := r.runFn
	r.mu.Unlock()
	if fn != nil {
		return fn(opts)
	}
	return r.SandboxSession.Run(ctx, opts)
}

func (r *recordingSandbox) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	r.mu.Lock()
	r.spawns = append(r.spawns, opts)
	r.mu.Unlock()
	return r.SandboxSession.Spawn(ctx, opts)
}

func (r *recordingSandbox) Description() string {
	return "Custom sandbox with Python and network access."
}

func execBash(t *testing.T, tool types.Tool, command string) Output {
	t.Helper()
	out, err := tool.Execute(context.Background(), map[string]interface{}{"command": command}, types.ToolExecutionOptions{ToolCallID: "test-call"})
	if err != nil {
		t.Fatalf("execute %q: %v", command, err)
	}
	return out.(Output)
}

func TestBashToolUsesSandboxDefaultWithoutSetupCommands(t *testing.T) {
	sb, workDir := newSandbox(t)
	rec := &recordingSandbox{SandboxSession: sb}
	tool, err := CreateBashTool(context.Background(), Options{Sandbox: rec})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.runs)+len(rec.spawns) != 0 {
		t.Fatalf("setup commands ran: %v %v", rec.runs, rec.spawns)
	}
	if tool.Name != "bash" || !strings.Contains(tool.Description, "sandbox's default working directory") || !strings.Contains(tool.Description, rec.Description()) {
		t.Fatalf("tool = %q / %q", tool.Name, tool.Description)
	}
	if got := execBash(t, tool, "pwd"); got != (Output{Stdout: workDir + "\n"}) {
		t.Fatalf("pwd = %+v", got)
	}
	if len(rec.spawns) != 1 || rec.spawns[0].Command != "pwd" || rec.spawns[0].WorkingDirectory != "" {
		t.Fatalf("spawns = %+v", rec.spawns)
	}
}

func TestBashToolRequiresSandbox(t *testing.T) {
	_, err := CreateBashTool(context.Background(), Options{})
	if !providerrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBashToolWorkingDirectory(t *testing.T) {
	for _, suffix := range []string{"custom/work/", "it's $(echo literal)", ""} {
		name := "root"
		if suffix != "" {
			name = suffix
		}
		t.Run(name, func(t *testing.T) {
			sb, base := newSandbox(t)
			dir := "/"
			if suffix != "" {
				dir = base + "/" + suffix
			}
			tool, err := CreateBashTool(context.Background(), Options{Sandbox: sb, WorkingDirectory: dir})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(tool.Description, "Every command starts in "+normalizePosix(dir)+". ") {
				t.Fatalf("description = %q", tool.Description)
			}
			if suffix == "" {
				return
			}
			if err := sb.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{Path: dir + "/input.txt", Content: "hello"}); err != nil {
				t.Fatal(err)
			}
			if got := execBash(t, tool, "cat input.txt"); got != (Output{Stdout: "hello"}) {
				t.Fatalf("cat = %+v", got)
			}
		})
	}
}

func TestBashToolRejectsInvalidOptionsBeforeChangingTheSession(t *testing.T) {
	sb, _ := newSandbox(t)
	rec := &recordingSandbox{SandboxSession: sb}
	check := func(name string, opts Options, field string) {
		t.Helper()
		opts.Sandbox = rec
		_, err := CreateBashTool(context.Background(), opts)
		var invalid *providerrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != field {
			t.Fatalf("%s: err = %v, want InvalidArgumentError for %s", name, err, field)
		}
		if len(rec.runs)+len(rec.spawns) != 0 {
			t.Fatalf("%s: the session was changed", name)
		}
	}
	check("relative working directory", Options{WorkingDirectory: "relative"}, "workingDirectory")
	timeouts, lengths := []int{0, -1}, []int{0, -1}
	// Values past the TS bounds exist only where int is 64 bits; this test
	// also builds for linux/386 and linux/arm.
	if math.MaxInt > MaxTimeoutMs {
		tooLong, unsafe := int64(MaxTimeoutMs)+1, maxSafeInteger+1
		timeouts = append(timeouts, int(tooLong))
		lengths = append(lengths, int(unsafe))
		if _, err := CreateBashTool(context.Background(), Options{Sandbox: rec, MaxOutputLength: intPtr(int(maxSafeInteger))}); err != nil {
			t.Fatalf("MAX_SAFE_INTEGER maxOutputLength rejected: %v", err)
		}
	}
	for _, v := range timeouts {
		check("timeout "+strconv.Itoa(v), Options{WorkingDirectory: "/app", TimeoutMs: intPtr(v)}, "timeout")
	}
	for _, v := range lengths {
		check("maxOutputLength "+strconv.Itoa(v), Options{MaxOutputLength: intPtr(v)}, "maxOutputLength")
	}
	if _, err := CreateBashTool(context.Background(), Options{Sandbox: rec, TimeoutMs: intPtr(2_147_483_647)}); err != nil {
		t.Fatalf("max timeout rejected: %v", err)
	}
}

func TestBashToolFailsWhenWorkingDirectoryCannotBeCreated(t *testing.T) {
	sb, _ := newSandbox(t)
	rec := &recordingSandbox{SandboxSession: sb, runFn: func(providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		return providerutils.SandboxRunResult{ExitCode: 1, Stderr: "Permission denied"}, nil
	}}
	_, err := CreateBashTool(context.Background(), Options{Sandbox: rec, WorkingDirectory: "/app"})
	if err == nil || !strings.Contains(err.Error(), "Failed to create working directory: Permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestBashToolSharesFilesAndResetsShellState(t *testing.T) {
	sb, _ := newSandbox(t)
	if err := sb.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{Path: "data/words.txt", Content: "pear\napple\npear\n"}); err != nil {
		t.Fatal(err)
	}
	first, _ := CreateBashTool(context.Background(), Options{Sandbox: sb})
	second, _ := CreateBashTool(context.Background(), Options{Sandbox: sb})

	if got := execBash(t, first, "cat data/words.txt | sort | uniq > report.txt"); got != (Output{}) {
		t.Fatalf("pipeline = %+v", got)
	}
	if got := execBash(t, second, "cat report.txt"); got != (Output{Stdout: "apple\npear\n"}) {
		t.Fatalf("second tool sees %+v", got)
	}
	// Shell state does not persist between calls; files do.
	execBash(t, first, "mkdir subdir; cd subdir; export NAME=value; touch saved")
	got := execBash(t, first, `printf "name=%s\n" "$NAME"; test -f subdir/saved`)
	if got != (Output{Stdout: "name=\n"}) {
		t.Fatalf("state = %+v", got)
	}
}

func TestBashToolPreservesStderrAndExitCode(t *testing.T) {
	sb, _ := newSandbox(t)
	tool, _ := CreateBashTool(context.Background(), Options{Sandbox: sb})
	got := execBash(t, tool, "echo partial; echo failure >&2; exit 7")
	if got != (Output{Stdout: "partial\n", Stderr: "failure\n", ExitCode: 7}) {
		t.Fatalf("got %+v", got)
	}
}

func TestBashToolOutputLimit(t *testing.T) {
	sb, _ := newSandbox(t)
	small, _ := CreateBashTool(context.Background(), Options{Sandbox: sb, MaxOutputLength: intPtr(4)})
	if got := execBash(t, small, "printf 123456789; printf abcdefghi >&2"); got != (Output{
		Stdout: "1234\n[Output truncated: 5 characters omitted.]",
		Stderr: "abcd\n[Output truncated: 5 characters omitted.]",
	}) {
		t.Fatalf("got %+v", got)
	}
	if got := execBash(t, small, "printf 1234"); got != (Output{Stdout: "1234"}) {
		t.Fatalf("at the limit: %+v", got)
	}
	if got := execBash(t, small, "printf 'héllo wörld'"); got.Stdout != "héll\n[Output truncated: 7 characters omitted.]" {
		t.Fatalf("multibyte: %+v", got)
	}

	def, _ := CreateBashTool(context.Background(), Options{Sandbox: sb})
	want := strings.Repeat("x", 30_000) + "\n[Output truncated: 1 characters omitted.]"
	if got := execBash(t, def, "head -c 30001 /dev/zero | tr '\\0' x"); got.Stdout != want {
		t.Fatalf("default limit: len=%d", len(got.Stdout))
	}
}

// Output is bounded while it streams, not after it has been buffered.
func TestBashToolDiscardsOutputBeyondTheLimitWhileStreaming(t *testing.T) {
	sb, _ := newSandbox(t)
	tool, _ := CreateBashTool(context.Background(), Options{Sandbox: sb, MaxOutputLength: intPtr(100)})
	got := execBash(t, tool, "head -c 50000000 /dev/zero | tr '\\0' y")
	if want := strings.Repeat("y", 100) + "\n[Output truncated: 49999900 characters omitted.]"; got.Stdout != want {
		t.Fatalf("stdout = %.60q... (len %d)", got.Stdout, len(got.Stdout))
	}
}

func TestBashToolTimeout(t *testing.T) {
	for _, withCallerContext := range []bool{false, true} {
		t.Run("caller context "+strconv.FormatBool(withCallerContext), func(t *testing.T) {
			sb, workDir := newSandbox(t)
			// 1 s leaves a loaded machine time to start the shell and print
			// "before" before the timeout; the command would run 20 s.
			tool, err := CreateBashTool(context.Background(), Options{Sandbox: sb, TimeoutMs: intPtr(1000)})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if withCallerContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}
			start := time.Now()
			out, err := tool.Execute(ctx, map[string]interface{}{"command": "echo before; sleep 20; echo finished > marker.txt"}, types.ToolExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := out.(Output); got.ExitCode != TimeoutExitCode || got.Stdout != "before\n" {
				t.Fatalf("got %+v", got)
			}
			if time.Since(start) > 15*time.Second {
				t.Fatal("the timeout did not stop the command")
			}
			if _, err := os.Stat(filepath.Join(workDir, "marker.txt")); !os.IsNotExist(err) {
				t.Fatalf("marker.txt exists after the command was stopped (stat err %v)", err)
			}
			if ctx.Err() != nil {
				t.Fatal("the caller's context was cancelled by the tool")
			}
			// Each execution gets a fresh timeout, even after one expired.
			if got := execBash(t, tool, "echo hello"); got != (Output{Stdout: "hello\n"}) {
				t.Fatalf("after timeout: %+v", got)
			}
		})
	}
}

// The timeout kills the whole process group, not just the shell.
func TestBashToolTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	sb, workDir := newSandbox(t)
	tool, _ := CreateBashTool(context.Background(), Options{Sandbox: sb, TimeoutMs: intPtr(300)})
	out := execBash(t, tool, "sleep 60 & echo $! > child.pid; wait")
	if out.ExitCode != TimeoutExitCode {
		t.Fatalf("got %+v", out)
	}
	raw, err := os.ReadFile(filepath.Join(workDir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child process %d survived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBashToolPropagatesCallerCancellation(t *testing.T) {
	sb, _ := newSandbox(t)
	for _, timeoutMs := range []*int{nil, intPtr(10_000)} {
		tool, _ := CreateBashTool(context.Background(), Options{Sandbox: sb, TimeoutMs: timeoutMs})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := tool.Execute(ctx, map[string]interface{}{"command": "echo hello"}, types.ToolExecutionOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("already cancelled: err = %v", err)
		}

		ctx, cancel = context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		start := time.Now()
		_, err := tool.Execute(ctx, map[string]interface{}{"command": "sleep 5"}, types.ToolExecutionOptions{})
		if !errors.Is(err, context.Canceled) || time.Since(start) > 4*time.Second {
			t.Fatalf("cancelled mid-command: err = %v after %v", err, time.Since(start))
		}
	}
}

func TestBashToolRejectsNonStringCommand(t *testing.T) {
	sb, _ := newSandbox(t)
	tool, _ := CreateBashTool(context.Background(), Options{Sandbox: sb})
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"command": 5}, types.ToolExecutionOptions{}); err == nil {
		t.Fatal("expected an error")
	}
}

// The byte budget is maxChars*utf8.UTFMax and must not overflow int on any
// architecture.
func TestBoundedOutputByteBudget(t *testing.T) {
	if got := newBoundedOutput(30_000).maxBytes; got != 30_000*utf8.UTFMax {
		t.Fatalf("maxBytes = %d", got)
	}
	if got := newBoundedOutput(int(maxScaledChars)).maxBytes; got != int(maxScaledChars)*utf8.UTFMax || got <= 0 {
		t.Fatalf("maxBytes at the scaling limit = %d", got)
	}
	if got := newBoundedOutput(math.MaxInt).maxBytes; got != math.MaxInt {
		t.Fatalf("maxBytes for math.MaxInt = %d", got)
	}
}
