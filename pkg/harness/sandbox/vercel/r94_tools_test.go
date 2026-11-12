package vercel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS abortSignal?.throwIfAborted() throws the abort reason (context.Cause),
// before any request: session run / spawn / read / write, the legacy
// provider and the template snapshot poll.
func TestR94toolsVercelThrowIfAbortedReason(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sess := NewSession(newTestSandbox(t, f, "sbx_abort"))
	reason := errors.New("stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)

	if _, err := sess.Run(ctx, providerutils.SandboxProcessOptions{Command: "echo hi"}); err != reason {
		t.Fatalf("run err = %v", err)
	}
	if _, err := sess.Spawn(ctx, providerutils.SandboxProcessOptions{Command: "echo hi"}); err != reason {
		t.Fatalf("spawn err = %v", err)
	}
	if _, err := sess.ReadBinaryFile(ctx, "/a"); err != reason {
		t.Fatalf("read err = %v", err)
	}
	if err := sess.WriteBinaryFile(ctx, "/d/a", []byte("x")); err != reason {
		t.Fatalf("write err = %v", err)
	}
	if _, err := sess.runPathScript(ctx, "true", "/a"); err != reason {
		t.Fatalf("path script err = %v", err)
	}
	legacy := NewLegacyProvider(LegacySettings{})
	if _, err := legacy.CreateSession(ctx, harness.CreateSandboxSessionOptions{}); err != reason {
		t.Fatalf("legacy create err = %v", err)
	}
	if _, err := legacy.ResumeSession(ctx, "s"); err != reason {
		t.Fatalf("legacy resume err = %v", err)
	}
	client := NewAPIClient(f.URL(), Credentials{Token: "tok", TeamID: "team", ProjectID: "proj"})
	if _, err := pollForTemplateSnapshotWithTimeout(ctx, client, "tpl", time.Second); err != reason {
		t.Fatalf("poll err = %v", err)
	}
}
