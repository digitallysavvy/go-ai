package local

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS 1d2c2ff2e4: Run with MaxOutputLength returns capped prefixes and the full
// lengths, after the command completed.
func TestPortToolsRunBoundedReturnsPrefixAndLengths(t *testing.T) {
	ctx := context.Background()
	s, err := newTestProvider(t).CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "bounded"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy(ctx) //nolint:errcheck
	res, err := s.Run(ctx, providerutils.SandboxProcessOptions{
		Command:         "seq 1 100000; printf error >&2; echo done > marker; exit 7",
		MaxOutputLength: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "1\n2\n" || res.Stderr != "erro" || res.StdoutLength != 588895 || res.StderrLength != 5 || res.ExitCode != 7 {
		t.Fatalf("res = %+v", res)
	}
	marker, err := s.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: "marker"})
	if err != nil || marker == nil || *marker != "done\n" {
		t.Fatalf("marker = %v, %v", marker, err)
	}
	if _, err := s.Run(ctx, providerutils.SandboxProcessOptions{Command: "true", MaxOutputLength: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
}
