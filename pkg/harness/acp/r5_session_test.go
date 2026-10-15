package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func finishFrame() map[string]any {
	return map[string]any{
		"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
	}
}

// TS: `debug` from the observability config rides on every `start`.
func TestDoPromptTurn_StartCarriesDebugConfig(t *testing.T) {
	captured := make(chan map[string]any, 1)
	srv := newServer(t, "tok", func(turn *bridgetest.Turn, start map[string]any) {
		captured <- start
		turn.Emit(finishFrame())
	})
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	enabled := true
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox,
		Observability: &harness.Observability{Debug: &harness.DebugConfig{Enabled: &enabled, Level: harness.DebugLevelDebug}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	start := <-captured
	dbg, _ := start["debug"].(map[string]any)
	if dbg["enabled"] != true || dbg["level"] != "debug" {
		t.Fatalf("start.debug = %v", start["debug"])
	}
	tsc, _ := start["turnStartConfig"].(map[string]any)
	if tsc["debug"] == nil {
		t.Fatalf("turnStartConfig.debug missing: %v", tsc)
	}
}

// TS: an abort asks the bridge to stop and the turn settles with the abort
// error once the bridge reports `finish`; the terminal event is still
// forwarded and the session can start the next prompt.
func TestDoPromptTurn_AbortSettlesOnBridgeFinish(t *testing.T) {
	aborted := make(chan struct{}, 1)
	started := make(chan *bridgetest.Turn, 2)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) { started <- turn },
		OnFrame: func(ft string) {
			if ft == "abort" {
				aborted <- struct{}{}
			}
		}})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{}, 1)
	c, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinish {
			finished <- struct{}{}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	turn := <-started
	cancel()
	select {
	case <-aborted:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received abort")
	}
	select {
	case <-c.Done():
		t.Fatal("turn settled before the bridge reported finish")
	case <-time.After(100 * time.Millisecond):
	}
	turn.Emit(finishFrame())
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not settle after finish")
	}
	if c.Err() == nil || !strings.Contains(c.Err().Error(), "canceled") {
		t.Fatalf("control.Err() = %v, want the abort error", c.Err())
	}
	select {
	case <-finished:
	default:
		t.Fatal("finish part was not forwarded")
	}
	// The turn is no longer in flight, so a new prompt is accepted.
	next, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("again"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatalf("next prompt: %v", err)
	}
	t2 := <-started
	t2.Emit(finishFrame())
	<-next.Done()
}

// TS: a pre-aborted signal rejects before any `start` is sent.
func TestDoPromptTurn_PreAbortedRejects(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) { t.Error("start must not be sent") })
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}}); err == nil {
		t.Fatal("expected the abort error")
	}
}

// TS validateACPLifecycleCompatibility: state without an authentication
// profile digest is incompatible.
func TestDoStart_ResumeWithoutAuthenticationProfileRejected(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	state, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	_ = json.Unmarshal(state.Data, &data)
	delete(data, "authenticationProfile")
	state2, _ := harness.NewResumeSessionState(state.HarnessID, data)
	_, err = h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox, ResumeFrom: state2})
	if err == nil || !strings.Contains(err.Error(), "incompatible with the configured authentication profile") {
		t.Fatalf("err = %v", err)
	}
}

// TS resolveACPInitialGuidanceApplied: an explicit false round-trips; absent
// means true on resume.
func TestLifecycleData_InitialGuidanceAppliedRoundTrips(t *testing.T) {
	f := false
	b, _ := json.Marshal(resumeStateData{InitialGuidanceApplied: &f})
	if !strings.Contains(string(b), `"initialGuidanceApplied":false`) {
		t.Fatalf("explicit false dropped: %s", b)
	}
	b, _ = json.Marshal(resumeStateData{})
	if strings.Contains(string(b), "initialGuidanceApplied") {
		t.Fatalf("absent value serialized: %s", b)
	}
}
