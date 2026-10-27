package acp

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type chmodSandbox struct {
	fakeSandbox
	runs     []string
	chmodExt int
}

func (s *chmodSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.runs = append(s.runs, opts.Command)
	if strings.HasPrefix(opts.Command, "chmod ") {
		return providerutils.SandboxRunResult{ExitCode: s.chmodExt, Stderr: "denied"}, nil
	}
	return s.fakeSandbox.Run(ctx, opts)
}

func TestWriteAuthenticationFilesRestrictsPermissions(t *testing.T) {
	s := &chmodSandbox{}
	err := writeAuthenticationFiles(context.Background(), s, "/home/u", []AuthenticationFile{
		{Path: ".fx/auth.json", Content: "a"}, {Path: "x y.json", Content: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.files["/home/u/.fx/auth.json"] != "a" {
		t.Fatalf("files = %v", s.files)
	}
	want := "chmod 600 -- '/home/u/.fx/auth.json' '/home/u/x y.json'"
	if len(s.runs) != 1 || s.runs[0] != want {
		t.Fatalf("runs = %q, want %q", s.runs, want)
	}
}

func TestWriteAuthenticationFilesRejectsTraversalBeforeWriting(t *testing.T) {
	for _, p := range []string{"", "/etc/passwd", "../x", "a/../../x"} {
		s := &chmodSandbox{}
		err := writeAuthenticationFiles(context.Background(), s, "/home/u", []AuthenticationFile{
			{Path: "ok.json", Content: "a"}, {Path: p, Content: "b"},
		})
		if err == nil || !strings.HasPrefix(err.Error(), "ACP authentication file path must be relative without traversal: ") {
			t.Fatalf("path %q: err = %v", p, err)
		}
		if len(s.files) != 0 {
			t.Fatalf("path %q: files written before validation: %v", p, s.files)
		}
	}
}

func TestWriteAuthenticationFilesChmodFailure(t *testing.T) {
	s := &chmodSandbox{chmodExt: 1}
	err := writeAuthenticationFiles(context.Background(), s, "/home/u", []AuthenticationFile{{Path: "a", Content: "x"}})
	if err == nil || err.Error() != "Failed to secure ACP authentication files (exit 1): denied" {
		t.Fatalf("err = %v", err)
	}
}
