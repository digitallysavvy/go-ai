//go:build unix

package toolbash

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS bash tool passes the abort signal to sandbox.run, which rejects with
// the abort reason: before the command starts and while it runs.
func TestR95toolsBashAbortReason(t *testing.T) {
	sandbox, _ := newSandbox(t)
	tool, err := CreateBashTool(context.Background(), Options{Sandbox: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("r95 stop")

	pre, cancelPre := context.WithCancelCause(context.Background())
	cancelPre(cause)
	if _, err := tool.Execute(pre, map[string]interface{}{"command": "true"}, types.ToolExecutionOptions{ToolCallID: "c"}); err != cause { //nolint:errorlint // the reason itself
		t.Fatalf("pre-aborted err = %v, want the abort reason", err)
	}

	mid, cancelMid := context.WithCancelCause(context.Background())
	time.AfterFunc(200*time.Millisecond, func() { cancelMid(cause) })
	if _, err := tool.Execute(mid, map[string]interface{}{"command": "sleep 5"}, types.ToolExecutionOptions{ToolCallID: "c"}); err != cause { //nolint:errorlint // the reason itself
		t.Fatalf("mid-run err = %v, want the abort reason", err)
	}
}
