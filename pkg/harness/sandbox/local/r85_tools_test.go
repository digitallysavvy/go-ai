//go:build !windows

package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS 5c247e9b85 stat / listDirectory / deleteFile and 95348b32c6 spawn
// stdin, on the Go-only local sandbox.
func TestR85toolsLocalPathOpsAndStdin(t *testing.T) {
	ctx := context.Background()
	sess, err := newTestProvider(t).CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "r85"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Destroy(ctx) //nolint:errcheck
	s := sess.(*Session)
	wd := s.DefaultWorkingDirectory()
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(wd, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(wd, "link")); err != nil {
		t.Fatal(err)
	}

	if st, err := s.Stat(ctx, "a.txt"); err != nil || st == nil || st.Type != "file" || st.Size != 5 {
		t.Fatalf("stat = %+v, %v", st, err)
	}
	if st, err := s.Stat(ctx, "link"); err != nil || st == nil || st.Type != "file" {
		t.Fatalf("stat link = %+v, %v", st, err)
	}
	if st, err := s.Stat(ctx, "a.txt/x"); err != nil || st != nil {
		t.Fatalf("stat under file = %+v, %v", st, err)
	}
	entries, err := s.ListDirectory(ctx, ".")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.Type
	}
	if want := map[string]string{"a.txt": "file", "d": "directory", "link": "symlink"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if e, err := s.ListDirectory(ctx, "missing"); err != nil || e != nil {
		t.Fatalf("list missing = %v, %v", e, err)
	}
	if _, err := s.ListDirectory(ctx, "a.txt"); err == nil {
		t.Fatal("listing a file must fail")
	}
	if ok, err := s.DeleteFile(ctx, "d"); ok || err == nil {
		t.Fatalf("delete dir = %v, %v", ok, err)
	}
	if ok, err := s.DeleteFile(ctx, "link"); !ok || err != nil {
		t.Fatalf("delete link = %v, %v", ok, err)
	}
	if ok, err := s.DeleteFile(ctx, "link"); ok || err != nil {
		t.Fatalf("delete again = %v, %v", ok, err)
	}

	proc, err := s.Restricted().Spawn(ctx, providerutils.SandboxProcessOptions{Command: "cat", Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	w, ok := proc.(providerutils.WritableSandboxProcess)
	if !ok {
		t.Fatal("a Stdin spawn must return a WritableSandboxProcess")
	}
	if _, err := io.WriteString(w.Stdin(), "piped"); err != nil {
		t.Fatal(err)
	}
	_ = w.Stdin().Close()
	out, _ := io.ReadAll(proc.Stdout())
	if res, err := proc.Wait(); err != nil || res.ExitCode != 0 || string(out) != "piped" {
		t.Fatalf("cat = %q, %+v, %v", out, res, err)
	}
	plain, err := s.Spawn(ctx, providerutils.SandboxProcessOptions{Command: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.(providerutils.WritableSandboxProcess); ok {
		t.Fatal("a spawn without Stdin must not expose stdin")
	}
	if res, err := plain.Wait(); err != nil || res.ExitCode != 0 {
		t.Fatalf("cat with closed stdin = %+v, %v", res, err)
	}
}
