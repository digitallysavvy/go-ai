package harnessutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports TS is-sandbox-write-protected-path.test.ts and
// write-protected-path-matching (71bef690f9).

var porttoolsEntries = []string{".agents/skills", ".claude", "config.json"}

type porttoolsSandbox struct {
	providerutils.SandboxSession
	runs   atomic.Int32
	result *providerutils.SandboxRunResult
}

func (s *porttoolsSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.runs.Add(1)
	if s.result != nil {
		return *s.result, nil
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", opts.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		return providerutils.SandboxRunResult{}, err
	}
	return providerutils.SandboxRunResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}, nil
}

func porttoolsMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func porttoolsWrite(t *testing.T, workDir, rel, content string) {
	t.Helper()
	full := filepath.Join(workDir, rel)
	porttoolsMust(t, os.MkdirAll(filepath.Dir(full), 0o755))
	porttoolsMust(t, os.WriteFile(full, []byte(content), 0o644))
}

func porttoolsSkill(t *testing.T, workDir string) {
	porttoolsWrite(t, workDir, ".agents/skills/test/SKILL.md", "---\nname: test\n---\n")
}

func porttoolsSymlink(t *testing.T, workDir, target, link string) {
	t.Helper()
	porttoolsMust(t, os.MkdirAll(filepath.Dir(filepath.Join(workDir, link)), 0o755))
	porttoolsMust(t, os.Symlink(target, filepath.Join(workDir, link)))
}

func TestPortToolsIsSandboxWriteProtectedPathCases(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T, w string)
		paths     []string
		protected bool
	}{
		{"an unprotected file", porttoolsSkill, []string{"TEST.md"}, false},
		{"a file inside a protected directory", porttoolsSkill, []string{".agents/skills/test/SKILL.md"}, true},
		{"a new file inside a missing protected directory", nil, []string{".claude/settings.json"}, true},
		{"a protected file", nil, []string{"config.json"}, true},
		{"a parent of a protected directory", porttoolsSkill, []string{".agents"}, true},
		{"a sibling sharing a protected name prefix", porttoolsSkill, []string{".agents/skills-other/file.md"}, false},
		{"any of several paths", porttoolsSkill, []string{"TEST.md", ".agents/skills/test/SKILL.md"}, true},
		{"an alias that points into a protected directory", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsSymlink(t, w, ".agents/skills", "skills")
		}, []string{"skills/test/SKILL.md"}, true},
		{"the target of a symlink inside a protected directory", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsMust(t, os.Mkdir(filepath.Join(w, "shared"), 0o755))
			porttoolsSymlink(t, w, "../../shared", ".agents/skills/shared")
		}, []string{"shared/new.md"}, true},
		{"a target reached through nested symlinks", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsMust(t, os.Mkdir(filepath.Join(w, "first"), 0o755))
			porttoolsMust(t, os.Mkdir(filepath.Join(w, "second"), 0o755))
			porttoolsSymlink(t, w, "../../first", ".agents/skills/first")
			porttoolsSymlink(t, w, "../second", "first/second")
		}, []string{"second/new.md"}, true},
		{"a file targeted by a symlink inside a protected directory", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsWrite(t, w, "notes.md", "notes\n")
			porttoolsSymlink(t, w, "../../notes.md", ".agents/skills/notes.md")
		}, []string{"notes.md"}, true},
		{"a hard-linked file while a protected path exists", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsMust(t, os.Link(filepath.Join(w, ".agents/skills/test/SKILL.md"), filepath.Join(w, "hard.md")))
		}, []string{"hard.md"}, true},
		{"a hard-linked file while no protected path exists", func(t *testing.T, w string) {
			porttoolsWrite(t, w, "one.md", "one\n")
			porttoolsMust(t, os.Link(filepath.Join(w, "one.md"), filepath.Join(w, "two.md")))
		}, []string{"two.md"}, false},
		{"any write while a protected directory holds a dangling symlink", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsSymlink(t, w, "missing", ".agents/skills/dangling")
		}, []string{"TEST.md"}, true},
		{"a looping symlink", func(t *testing.T, w string) {
			porttoolsSymlink(t, w, "loop-b", "loop-a")
			porttoolsSymlink(t, w, "loop-a", "loop-b")
		}, []string{"loop-a"}, true},
		{"a normalized parent component after a symlink", func(t *testing.T, w string) {
			porttoolsSkill(t, w)
			porttoolsMust(t, os.MkdirAll(filepath.Join(w, "deep/nested"), 0o755))
			porttoolsSymlink(t, w, "deep/nested", "link")
		}, []string{"link/../.agents/skills/new.md"}, true},
		{"any write when protected directories exceed the scan limit", func(t *testing.T, w string) {
			porttoolsMust(t, os.MkdirAll(filepath.Join(w, ".agents/skills/many"), 0o755))
			for i := range 2001 {
				porttoolsWrite(t, w, fmt.Sprintf(".agents/skills/many/%d.md", i), "")
			}
		}, []string{"TEST.md"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, workDir)
			}
			paths := make([]string, len(tc.paths))
			for i, p := range tc.paths {
				paths[i] = workDir + "/" + p
			}
			got, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{
				Sandbox: &porttoolsSandbox{}, WorkDir: workDir, Paths: paths, WriteProtectedPaths: porttoolsEntries,
			})
			if err != nil || got != tc.protected {
				t.Fatalf("protected = %v (err %v), want %v", got, err, tc.protected)
			}
		})
	}
}

func TestPortToolsIsSandboxWriteProtectedPathUsesSingleCommand(t *testing.T) {
	workDir, err := filepath.EvalSymlinks(t.TempDir())
	porttoolsMust(t, err)
	sandbox := &porttoolsSandbox{}
	got, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{
		Sandbox: sandbox, WorkDir: workDir, Paths: []string{workDir + "/TEST.md"}, WriteProtectedPaths: porttoolsEntries,
	})
	if err != nil || got || sandbox.runs.Load() != 1 {
		t.Fatalf("got=%v err=%v runs=%d", got, err, sandbox.runs.Load())
	}
}

func TestPortToolsIsSandboxWriteProtectedPathFailedOrMalformedOutputIsProtected(t *testing.T) {
	workDir := t.TempDir()
	for _, result := range []providerutils.SandboxRunResult{
		{ExitCode: 1, Stderr: "failed"},
		{Stdout: "W not-base64\nE\n"},
		{},
	} {
		result := result
		got, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{
			Sandbox: &porttoolsSandbox{result: &result}, WorkDir: workDir, Paths: []string{workDir + "/TEST.md"}, WriteProtectedPaths: porttoolsEntries,
		})
		if err != nil || !got {
			t.Errorf("%+v: got=%v err=%v", result, got, err)
		}
	}
}

func TestPortToolsIsSandboxWriteProtectedPathEmptyAndRelativePathsAreProtected(t *testing.T) {
	workDir := t.TempDir()
	sandbox := &porttoolsSandbox{}
	for _, paths := range [][]string{{}, {""}, {"TEST.md"}} {
		got, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{
			Sandbox: sandbox, WorkDir: workDir, Paths: paths, WriteProtectedPaths: porttoolsEntries,
		})
		if err != nil || !got {
			t.Errorf("%v: got=%v err=%v", paths, got, err)
		}
	}
	if sandbox.runs.Load() != 0 {
		t.Fatalf("sandbox ran %d times", sandbox.runs.Load())
	}
}

func TestPortToolsIsSandboxWriteProtectedPathRejectsInvalidEntries(t *testing.T) {
	workDir := t.TempDir()
	for _, entry := range []string{"../outside", "", "/abs", "a//b", "a/./b", "a/"} {
		_, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{
			Sandbox: &porttoolsSandbox{}, WorkDir: workDir, Paths: []string{workDir + "/TEST.md"}, WriteProtectedPaths: []string{entry},
		})
		if err == nil || !strings.HasPrefix(err.Error(), "Write-protected path entries must be relative paths") {
			t.Errorf("entry %q: err = %v", entry, err)
		}
	}
	// No entries: nothing is protected, without touching the sandbox.
	sandbox := &porttoolsSandbox{}
	got, err := IsSandboxWriteProtectedPath(t.Context(), IsSandboxWriteProtectedPathOptions{Sandbox: sandbox, WorkDir: workDir, Paths: []string{workDir + "/x"}})
	if err != nil || got || sandbox.runs.Load() != 0 {
		t.Fatalf("got=%v err=%v runs=%d", got, err, sandbox.runs.Load())
	}
}

func TestPortToolsMatchesWriteProtectedPath(t *testing.T) {
	protected := []string{"/w/.fx", "/w/.agents/skills"}
	for _, tc := range []struct {
		target    string
		multiLink bool
		exists    bool
		want      bool
	}{
		{"/w/notes.md", false, true, false},
		{"/w/.fx/settings.json", false, true, true},
		{"/w/.fx", false, true, true},
		{"/w/.agents", false, true, true},
		{"/w/.fx-other/a", false, true, false},
		{"/w/two.md", true, true, true},
		{"/w/two.md", true, false, false},
		{"/", false, false, true},
	} {
		got := matchesWriteProtectedPath([]writeProtectionTarget{{path: tc.target, multiLink: tc.multiLink}}, protected, tc.exists)
		if got != tc.want {
			t.Errorf("%s multi=%v exists=%v: got %v", tc.target, tc.multiLink, tc.exists, got)
		}
	}
}
