package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// trailingSlashHomeSandbox reports HOME with a trailing slash.
type trailingSlashHomeSandbox struct{ *fakeSandbox }

func (s *trailingSlashHomeSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if opts.Command == `printf "%s" "$HOME"` {
		return providerutils.SandboxRunResult{ExitCode: 0, Stdout: "/home/vercel-sandbox/"}, nil
	}
	return s.fakeSandbox.Run(ctx, opts)
}

func (s *trailingSlashHomeSandbox) Restricted() providerutils.SandboxSession { return s }

// TS builds the skills and bootstrap directories with path.posix.join and
// path.posix.resolve, which normalize a HOME with a trailing slash.
func TestR43SpawnPathsNormalizeHomeTrailingSlash(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
	t.Cleanup(srv.Close)
	sandbox := &trailingSlashHomeSandbox{newFakeSandbox(srv)}
	h, err := CreateOpenCode()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session",
		SandboxSession: sandbox,
	})
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) == 0 {
		t.Fatal("expected a spawn command")
	}
	cmd := sandbox.spawnCommands[0]
	if !strings.Contains(cmd, "--skills-dir '/home/vercel-sandbox/.agents/skills'") || strings.Contains(cmd, "//") {
		t.Fatalf("spawn command = %q, want normalized paths", cmd)
	}
}
