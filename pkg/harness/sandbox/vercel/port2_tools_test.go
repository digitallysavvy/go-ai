package vercel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func port2toolsStr(s string) *string { return &s }

// TS vercel-sandbox-session.test.ts "cancels an in-flight capped command and
// both pending requests": cancelling a capped run whose log stream is still
// open returns the abort reason, aborts the pending log request and kills the
// command exactly once.
func TestPort2toolsSessionRunBoundedCancelsInFlightCommand(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.blockLogs = true
	f.logsStarted = make(chan struct{})
	f.logsEnded = make(chan struct{}, 4)
	f.CommandFn = func(_, _ string, _ []string, _ string, _ map[string]string) CommandScript {
		return CommandScript{Stdout: "partial"}
	}
	sbx := newTestSandbox(t, f, "sbx_bounded_inflight")
	reason := errors.New("cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	type outcome struct {
		result providerutils.SandboxRunResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := NewSession(sbx).Run(ctx, providerutils.SandboxProcessOptions{Command: "command", MaxOutputLength: 4})
		done <- outcome{r, err}
	}()
	select {
	case <-f.logsStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("log stream never started")
	}
	cancel(reason)
	select {
	case got := <-done:
		if !errors.Is(got.err, reason) {
			t.Fatalf("err = %v, want the abort reason", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	select {
	case <-f.logsEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("the pending log request was not aborted")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		kills := f.kills
		f.mu.Unlock()
		if kills == 1 {
			break
		}
		if kills > 1 || time.Now().After(deadline) {
			t.Fatalf("kills = %d, want exactly 1", kills)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TS `templateName ?? derivedTemplateName`: only an absent name falls back,
// so an explicit empty name replaces the derived one.
func TestPort2toolsTemplateNameEmptyStringIsNotUnset(t *testing.T) {
	create := func(name *string) string {
		f := newFakeServer()
		defer f.Close()
		f.snapshotOnStop = true
		_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
			Credentials: testCreds(), BaseURL: f.URL(), Runtime: "node24", SandboxID: "live", TemplateName: name,
			Template: &Template{Identity: "recipe-one", Prepare: func(context.Context, providerutils.SandboxSession) error { return nil }},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.createRequests) < 1 {
			t.Fatal("no create request")
		}
		return f.createRequests[0].Name
	}
	if got := create(nil); !strings.HasPrefix(got, "ai-sdk-harness-v3-") {
		t.Fatalf("absent name: template name = %q, want the derived name", got)
	}
	if got := create(port2toolsStr("")); strings.HasPrefix(got, "ai-sdk-harness-v3-") {
		t.Fatalf("empty name: template name = %q, the derived name must not be used", got)
	}
	if got := create(port2toolsStr("app-template")); got != "app-template" {
		t.Fatalf("explicit name: template name = %q", got)
	}
}
