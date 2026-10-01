package vercel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRemoteProcessWait_HonorsOwnContextWhileLogDrainIsStuck is the
// regression test for bug-review/R3-2: RemoteProcess.Wait(ctx) is shaped to
// be bounded by its own ctx, but after GetCommand(ctx, ..., wait=true)
// returns it used to block unconditionally on <-p.logsDone, with no select
// against ctx.Done(). logsDone is only closed once consumeLogs's GetLogs
// call returns — a goroutine started by SpawnCommand against a *different*,
// typically longer-lived context than whatever ctx a later, independent Wait
// call is given. If the log stream is slow or stuck, Wait used to hang well
// past its own context's deadline/cancellation instead of returning
// ctx.Err() promptly.
//
// This constructs a RemoteProcess directly (same package) with logsDone a
// channel that is deliberately never closed — simulating a log stream that
// never finishes draining — against a fake server whose GetCommand endpoint
// answers immediately (the command itself has already finished). Wait is
// then called with a context that times out quickly; it must return
// context.DeadlineExceeded promptly, not hang.
func TestRemoteProcessWait_HonorsOwnContextWhileLogDrainIsStuck(t *testing.T) {
	const sessionID = "sess_test"
	const cmdID = "cmd_test"
	zero := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GetCommand(wait=true): the command has already finished
		// server-side — mirrors the bug report's repro ("GetCommand returns
		// immediately (command finished), logsDone deliberately left open").
		writeJSON(w, 200, commandResponse{Command: commandWire{ID: cmdID, SessionID: sessionID, ExitCode: &zero}})
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
	p := &RemoteProcess{
		client: client, sessionID: sessionID, cmdID: cmdID,
		stdout: newStreamBuffer(), stderr: newStreamBuffer(),
		// Deliberately never closed: simulates a log stream still draining
		// (or stuck) past this Wait call's own deadline.
		logsDone: make(chan struct{}),
		exitCode: -1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	type waitResult struct {
		code int
		err  error
	}
	done := make(chan waitResult, 1)
	go func() {
		code, err := p.Wait(ctx)
		done <- waitResult{code, err}
	}()

	select {
	case r := <-done:
		if !errors.Is(r.err, context.DeadlineExceeded) {
			t.Fatalf("Wait returned err = %v, want context.DeadlineExceeded", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not honor its own context: still blocked on the stuck log drain well past its 100ms deadline")
	}
}
