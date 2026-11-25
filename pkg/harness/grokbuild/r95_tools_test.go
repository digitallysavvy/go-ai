package grokbuild

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// TS prepareGrokBuildSandbox starts with abortSignal?.throwIfAborted(): the
// abort reason itself.
func TestR95toolsPrepareSandboxAbortReason(t *testing.T) {
	cause := errors.New("r95 stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if err := PrepareSandbox(ctx, acp.PrepareSandboxOptions{}); err != cause { //nolint:errorlint // TS throws the reason itself
		t.Fatalf("err = %v, want the abort reason", err)
	}
}
