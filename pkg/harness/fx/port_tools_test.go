package fx

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// porttoolsProtectionSandbox answers the host's write-protection scripts with
// real bash against workDir, and the runtime's own realpath commands with the
// path they ask about.
func porttoolsProtectionSandbox(sb *fakeSandbox, workDir string, protectionRuns *int, mu *sync.Mutex) {
	sb.handle = func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "__AI_SDK_WRITE_PROTECTION_FRAME__") {
			mu.Lock()
			*protectionRuns++
			mu.Unlock()
			cmd := exec.CommandContext(ctx, "bash", "-c", opts.Command)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			code := 0
			if err := cmd.Run(); err != nil {
				code = 1
			}
			return providerutils.SandboxRunResult{Stdout: stdout.String(), ExitCode: code}, nil
		}
		if strings.Contains(opts.Command, "realpath") {
			out := workDir + "\x00"
			switch {
			case strings.Contains(opts.Command, "settings.json"):
				out = workDir + "/.fx/settings.json\x00"
			case strings.Contains(opts.Command, "notes.md"):
				out = workDir + "/notes.md\x00"
			}
			return providerutils.SandboxRunResult{Stdout: b64(out)}, nil
		}
		return providerutils.SandboxRunResult{}, nil
	}
}

// TS 71bef690f9 fx-harness.test.ts "requires approval=$approval for an
// allow-edits write to $filePath".
func TestPortToolsAllowEditsWriteApprovalForProtectedPaths(t *testing.T) {
	for _, tc := range []struct {
		filePath string
		approval bool
	}{
		{"notes.md", false},
		{".fx/settings.json", true},
	} {
		t.Run(tc.filePath, func(t *testing.T) {
			workDir, err := evalSymlinksDir(t)
			if err != nil {
				t.Fatal(err)
			}
			h, m := newMockHarness(t, Settings{})
			sb := newFakeSandbox()
			var mu sync.Mutex
			runs := 0
			porttoolsProtectionSandbox(sb, workDir, &runs, &mu)
			m.setRun(func(_ context.Context, o runtimeOptions) error {
				_, _ = invoke(o, "write", "write-id", map[string]any{"file_path": tc.filePath, "content": "hello"})
				return nil
			})
			start := harness.StartOptions{SandboxSession: sb, SessionID: "write-approval", SessionWorkDir: workDir, PermissionMode: harness.PermissionModeAllowEdits}
			s, err := h.DoStart(context.Background(), start)
			if err != nil {
				t.Fatal(err)
			}
			defer s.DoDestroy(context.Background()) //nolint:errcheck
			approval := newSignal()
			events := &partLog{on: func(p harness.StreamPart) {
				if p.PartType() == harness.PartTypeToolApprovalRequest {
					approval.fire()
				}
			}}
			control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: events.emit})
			if err != nil {
				t.Fatal(err)
			}
			if tc.approval {
				approval.wait(t)
				sb.mu.Lock()
				_, written := sb.files[workDir+"/"+tc.filePath]
				sb.mu.Unlock()
				if written {
					t.Fatal("file written before approval")
				}
				if err := control.(harness.ToolApprovalSubmitter).SubmitToolApproval(context.Background(), harness.ToolApprovalSubmission{ApprovalID: "fx:write-id", Approved: true}); err != nil {
					t.Fatal(err)
				}
			}
			_ = waitDone(t, control)
			want := 0
			if tc.approval {
				want = 1
			}
			if n := len(events.ofType(harness.PartTypeToolApprovalRequest)); n != want {
				t.Fatalf("approval requests = %d, want %d", n, want)
			}
			mu.Lock()
			defer mu.Unlock()
			if runs != 1 {
				t.Fatalf("protection checks = %d, want 1 (decided once per call)", runs)
			}
		})
	}
}

// A write whose file_path is not a string fails closed.
func TestPortToolsAllowEditsNonStringFilePathNeedsApproval(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	sb := newFakeSandbox()
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		_, _ = invoke(o, "write", "write-id", map[string]any{"file_path": 7, "content": "x"})
		return nil
	})
	start := startOpts(sb, "write-nonstring")
	start.PermissionMode = harness.PermissionModeAllowEdits
	s, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(context.Background()) //nolint:errcheck
	approval := newSignal()
	if _, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeToolApprovalRequest {
			approval.fire()
		}
	}}); err != nil {
		t.Fatal(err)
	}
	approval.wait(t)
}

func evalSymlinksDir(t *testing.T) (string, error) {
	t.Helper()
	return filepath.EvalSymlinks(t.TempDir())
}

// The sandbox-ACP runtime hands the bridge the fx configuration directory as a
// write-protected path (.agents/skills is always protected by the bridge).
func TestPortToolsACPRuntimeWriteProtectedPaths(t *testing.T) {
	if got := BuildConfig(Settings{}).WriteProtectedPaths; !reflect.DeepEqual(got, []string{".fx"}) {
		t.Fatalf("WriteProtectedPaths = %v", got)
	}
}
