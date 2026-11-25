package acp

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/bridgeauthkeys"
)

// TS acp-v1-harness.ts calls startOptions.abortSignal?.throwIfAborted()
// after prepareSandbox: an abort during preparation rejects the start with
// the abort reason.
func TestR95toolsPrepareSandboxAbortReason(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: bridgeauthkeys.Testing().PublicKey})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	cause := errors.New("r95 stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.PrepareSandbox = func(context.Context, PrepareSandboxOptions) error {
			cancel(cause)
			return nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.DoStart(ctx, harness.StartOptions{SessionID: "s", SessionWorkDir: "/workspace/p", SandboxSession: sandbox})
	if err != cause { //nolint:errorlint // TS throws the reason itself
		t.Fatalf("DoStart err = %v, want the abort reason", err)
	}
}
