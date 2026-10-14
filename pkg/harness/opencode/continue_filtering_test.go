package opencode

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func finishPayload() map[string]any {
	return map[string]any{
		"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
	}
}

// TS #23342: built-in tool filtering is sent as `inactiveBuiltinToolNames`
// (every tool the filtering policy excludes, in catalog order) instead of the
// shared `builtinToolFiltering` field.
func TestDoPromptTurn_SendsInactiveBuiltinToolNames(t *testing.T) {
	cases := []struct {
		name      string
		filtering *harness.BuiltinToolFiltering
		want      any // nil => key absent
	}{
		{"no filtering", nil, nil},
		{"deny", &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{"bash", "webfetch"}}, []any{"bash", "webfetch"}},
		{"allow", &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow, ToolNames: []string{"read", "write", "edit", "glob", "grep", "ls", "skill", "todowrite", "agent", "webfetch", "bash"}}, []any{"askUserQuestions"}},
		{"deny nothing", &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{}}, []any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var captured map[string]any
			srv := newServer(t, "tok", func(turn *bridgetest.Turn, start map[string]any) {
				mu.Lock()
				captured = start
				mu.Unlock()
				turn.Emit(finishPayload())
			})
			h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return "tok" }})
			if err != nil {
				t.Fatal(err)
			}
			sess, err := h.DoStart(context.Background(), harness.StartOptions{
				SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv), BuiltinToolFiltering: tc.filtering,
			})
			if err != nil {
				t.Fatalf("DoStart: %v", err)
			}
			t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
			control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-control.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("turn did not finish")
			}
			mu.Lock()
			defer mu.Unlock()
			if _, has := captured["builtinToolFiltering"]; has {
				t.Error("start frame must not carry builtinToolFiltering")
			}
			got, has := captured["inactiveBuiltinToolNames"]
			if tc.want == nil {
				if has {
					t.Errorf("inactiveBuiltinToolNames = %v, want absent", got)
				}
				return
			}
			if !has || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("inactiveBuiltinToolNames = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDoStart_RejectsSplitWriteEditFiltering(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) {})
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return "tok" }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv),
		BuiltinToolFiltering: &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{"write"}},
	})
	var capErr *harness.CapabilityUnsupportedError
	if !errors.As(err, &capErr) {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
	if !harness.SupportsBuiltinToolFiltering(h) {
		t.Error("opencode must declare supportsBuiltinToolFiltering")
	}
}

// TS #23331: a continue after a turn paused for host input must not wait for
// the paused turn to drain, and must send no new start frame for a fresh
// session.
func TestDoContinueTurn_DoesNotWaitForPausedTurn(t *testing.T) {
	var starts int32
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) { atomic.AddInt32(&starts, 1) })
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return "tok" }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatal(err)
	}
	// The prompt turn never finishes (paused for host input).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := sess.DoContinueTurn(ctx, harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DoContinueTurn: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("DoContinueTurn deadlocked waiting for the paused turn to drain")
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&starts); got != 1 {
		t.Fatalf("starts = %d, want 1", got)
	}
}

// TS #23331: on a rerun-resumed session only the first continue (or prompt)
// sends a start frame.
func TestDoContinueTurn_RerunSendsStartOnce(t *testing.T) {
	var starts int32
	srv := newServer(t, "tok", func(turn *bridgetest.Turn, _ map[string]any) {
		atomic.AddInt32(&starts, 1)
		turn.Emit(finishPayload())
	})
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := harness.NewResumeSessionState("opencode", map[string]any{"openCodeSessionId": "oc-1"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv), ResumeFrom: resume})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	first, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first continue did not finish")
	}
	if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&starts); got != 1 {
		t.Fatalf("starts = %d, want 1", got)
	}
}
