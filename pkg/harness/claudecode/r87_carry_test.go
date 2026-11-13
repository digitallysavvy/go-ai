package claudecode_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// Port of TS 3bf70f83bc `claude-code-start.integration.test.ts`: a scripted
// bridge over a real WebSocket that can drop the connection when it receives
// a start, accept it first, delay the reconnect hello, and leave a
// user-message unanswered.

type r87carryTurnOpts struct {
	drop                   bool
	acceptBeforeDrop       bool
	finishBeforeReconnect  bool
	notIdempotent          bool
	startDuringReconnect   bool
	submitDuringReconnect  bool
	submitBeforeFirstEvent bool
}

type r87carryFrame struct {
	Leg     int
	Type    string
	StartID string
}

type r87carryConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *r87carryConn) send(v any) {
	data, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = websocket.Message.Send(c.ws, string(data))
}

// close sends a close frame with code 1001, then closes the socket.
func (c *r87carryConn) close(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.PayloadType = websocket.CloseFrame
	_, _ = c.ws.Write(append([]byte{0x03, 0xe9}, reason...))
	_ = c.ws.Close()
}

type r87carryBridge struct {
	opts r87carryTurnOpts

	mu           sync.Mutex
	frames       []r87carryFrame
	accepted     map[string]bool
	legs         int
	state        string
	lastSeq      float64
	first        *r87carryConn
	reconnectHi  func()
	reconnecting chan struct{}
}

func (b *r87carryBridge) hello(c *r87carryConn) {
	b.mu.Lock()
	state, lastSeq := b.state, b.lastSeq
	b.mu.Unlock()
	c.send(map[string]any{
		"type": "bridge-hello", "state": state, "lastSeq": lastSeq,
		"capabilities": map[string]any{
			"idempotentStart":                   !b.opts.notIdempotent,
			"experimental_userMessageResponses": b.opts.submitDuringReconnect || b.opts.submitBeforeFirstEvent,
		},
	})
}

func (b *r87carryBridge) finish(c *r87carryConn) {
	b.mu.Lock()
	b.state, b.lastSeq = "waiting", 1
	b.mu.Unlock()
	c.send(map[string]any{
		"type": "finish", "seq": 1,
		"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage":   map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
	})
}

func (b *r87carryBridge) handle(ws *websocket.Conn) {
	// Public-key authentication first (r91, TS c849d37dac): challenge, then
	// the host's proof, before bridge-hello.
	pub, err := bridge.ParseBridgeAuthPublicKey(claudecode.R91carryTestKeys.PublicKey)
	if err != nil {
		return
	}
	challenge := bridge.NewBridgeAuthChallenge()
	if websocket.Message.Send(ws, bridge.SerializeBridgeAuthChallengeFrame(challenge)) != nil {
		return
	}
	var proof []byte
	if websocket.Message.Receive(ws, &proof) != nil || !bridge.VerifyBridgeAuthProof(pub, challenge, proof, false) {
		_ = ws.Close()
		return
	}
	c := &r87carryConn{ws: ws}
	b.mu.Lock()
	b.legs++
	leg := b.legs
	if leg == 1 {
		b.first = c
	}
	delayHello := (b.opts.startDuringReconnect || b.opts.submitDuringReconnect) && leg == 2
	if delayHello {
		b.reconnectHi = func() { b.hello(c) }
	}
	b.mu.Unlock()
	if delayHello {
		close(b.reconnecting)
	} else {
		b.hello(c)
	}
	for {
		var raw string
		if err := websocket.Message.Receive(ws, &raw); err != nil {
			return
		}
		var msg struct {
			Type      string `json:"type"`
			StartID   string `json:"startId"`
			MessageID string `json:"messageId"`
		}
		_ = json.Unmarshal([]byte(raw), &msg)
		b.mu.Lock()
		b.frames = append(b.frames, r87carryFrame{Leg: leg, Type: msg.Type, StartID: msg.StartID})
		anyAccepted := len(b.accepted) > 0
		b.mu.Unlock()
		switch msg.Type {
		case "resume":
			if anyAccepted {
				b.finish(c)
			}
		case "user-message":
			if b.opts.submitBeforeFirstEvent && leg == 1 {
				continue
			}
			c.send(map[string]any{"type": "user-message-response", "messageId": msg.MessageID, "accepted": anyAccepted})
			b.mu.Lock()
			running := b.state == "running"
			b.mu.Unlock()
			if anyAccepted && running {
				b.finish(c)
			}
		case "start":
			dropLeg := 1
			if b.opts.startDuringReconnect {
				dropLeg = 2
			}
			b.mu.Lock()
			if b.opts.drop && leg == dropLeg {
				if b.opts.acceptBeforeDrop {
					b.accepted[msg.StartID] = true
					if b.opts.finishBeforeReconnect {
						b.state, b.lastSeq = "waiting", 1
					} else {
						b.state, b.lastSeq = "running", 0
					}
				}
				b.mu.Unlock()
				c.close("leg dropped")
				continue
			}
			if b.accepted[msg.StartID] {
				b.mu.Unlock()
				continue
			}
			b.accepted[msg.StartID] = true
			b.state = "running"
			b.mu.Unlock()
			if !b.opts.submitDuringReconnect && !b.opts.submitBeforeFirstEvent {
				b.finish(c)
			}
		}
	}
}

func (b *r87carryBridge) snapshot() (frames []r87carryFrame, accepted int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]r87carryFrame(nil), b.frames...), len(b.accepted)
}

type r87carryEvents struct {
	mu  sync.Mutex
	got []string
}

func (e *r87carryEvents) emit(p harness.StreamPart) {
	e.mu.Lock()
	e.got = append(e.got, p.PartType())
	e.mu.Unlock()
}

func (e *r87carryEvents) list() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.got...)
}

type r87carryTurn struct {
	bridge  *r87carryBridge
	control harness.PromptControl
	events  *r87carryEvents
	submit  chan error
}

func r87carryStartTurn(t *testing.T, opts r87carryTurnOpts) *r87carryTurn {
	t.Helper()
	b := &r87carryBridge{opts: opts, accepted: map[string]bool{}, state: "waiting", reconnecting: make(chan struct{})}
	srv := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: b.handle})
	t.Cleanup(srv.Close)
	sandbox := &testSandbox{
		Sandbox: bridgetest.NewSandbox(), home: "/home/agent", id: "test-sandbox", workDir: "/workdir",
		endpoint: harness.PortEndpoint{URL: "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/"}, ports: []int{4319},
	}
	wireSpawn(sandbox)
	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:   2 * time.Second,
		TurnStartTimeout: 2 * time.Second,
		Reconnect:        bridge.ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, MaxElapsed: 3 * time.Second},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := h.DoStart(ctx, harness.StartOptions{SessionID: "s", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = sess.DoDestroy(context.Background())
	})
	if opts.startDuringReconnect {
		b.mu.Lock()
		first := b.first
		b.mu.Unlock()
		first.close("idle drop")
		r87carryWait(t, b.reconnecting)
	}
	events := &r87carryEvents{}
	control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: events.emit, TurnSettings: harness.TurnSettings{Model: "m"}})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	turn := &r87carryTurn{bridge: b, control: control, events: events, submit: make(chan error, 1)}
	submit := func() {
		submitter, ok := control.(harness.UserMessageSubmitter)
		if !ok {
			t.Fatal("control does not accept user messages")
		}
		go func() { turn.submit <- submitter.SubmitUserMessage(ctx, "Follow-up") }()
	}
	switch {
	case opts.submitBeforeFirstEvent:
		submit()
	case opts.submitDuringReconnect:
		r87carryWait(t, b.reconnecting)
		submit()
		// Let the submission reach the disconnected channel first.
		time.Sleep(20 * time.Millisecond)
		b.mu.Lock()
		hello := b.reconnectHi
		b.mu.Unlock()
		hello()
	case opts.startDuringReconnect:
		b.mu.Lock()
		hello := b.reconnectHi
		b.mu.Unlock()
		hello()
	}
	return turn
}

func r87carryWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// done waits for the turn and returns its error.
func (tr *r87carryTurn) done(t *testing.T) error {
	t.Helper()
	r87carryWait(t, tr.control.Done())
	return tr.control.Err()
}

func (tr *r87carryTurn) mustFinish(t *testing.T) {
	t.Helper()
	if err := tr.done(t); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if tr.bridge.opts.submitBeforeFirstEvent || tr.bridge.opts.submitDuringReconnect {
		select {
		case err := <-tr.submit:
			if err != nil {
				t.Fatalf("SubmitUserMessage: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the user message was never answered")
		}
	}
	if got := tr.events.list(); !reflect.DeepEqual(got, []string{"finish"}) {
		t.Fatalf("events = %q, want [finish]", got)
	}
}

func r87carryTypes(frames []r87carryFrame, leg int) []string {
	var out []string
	for _, f := range frames {
		if leg == 0 || f.Leg == leg {
			out = append(out, f.Type)
		}
	}
	return out
}

func r87carryStarts(frames []r87carryFrame) []r87carryFrame {
	var out []r87carryFrame
	for _, f := range frames {
		if f.Type == "start" {
			out = append(out, f)
		}
	}
	return out
}

func TestR87carryRetriesLostStartBeforePendingUserMessage(t *testing.T) {
	for _, opts := range []r87carryTurnOpts{
		{drop: true, submitBeforeFirstEvent: true},
		{drop: true, submitDuringReconnect: true},
	} {
		turn := r87carryStartTurn(t, opts)
		turn.mustFinish(t)
		frames, accepted := turn.bridge.snapshot()
		if got := r87carryTypes(frames, 2); !reflect.DeepEqual(got, []string{"resume", "start", "user-message"}) {
			t.Fatalf("%+v: leg 2 frames = %q", opts, got)
		}
		if accepted != 1 {
			t.Fatalf("%+v: accepted = %d", opts, accepted)
		}
	}
}

func TestR87carryStartsWithoutDrop(t *testing.T) {
	turn := r87carryStartTurn(t, r87carryTurnOpts{})
	turn.mustFinish(t)
	frames, accepted := turn.bridge.snapshot()
	if got := r87carryTypes(frames, 0); !reflect.DeepEqual(got, []string{"start"}) || accepted != 1 {
		t.Fatalf("frames = %q, accepted = %d", got, accepted)
	}
	if frames[0].StartID == "" {
		t.Fatal("the start carries no startId")
	}
}

func TestR87carryResendsLostStartWithSameID(t *testing.T) {
	turn := r87carryStartTurn(t, r87carryTurnOpts{drop: true})
	turn.mustFinish(t)
	frames, accepted := turn.bridge.snapshot()
	starts := r87carryStarts(frames)
	if len(starts) != 2 || starts[0].Leg != 1 || starts[1].Leg != 2 || starts[0].StartID == "" || starts[1].StartID != starts[0].StartID || accepted != 1 {
		t.Fatalf("starts = %+v, accepted = %d", starts, accepted)
	}
}

func TestR87carryStartDuringIdleReconnect(t *testing.T) {
	for _, notIdempotent := range []bool{false, true} {
		turn := r87carryStartTurn(t, r87carryTurnOpts{startDuringReconnect: true, notIdempotent: notIdempotent})
		turn.mustFinish(t)
		frames, accepted := turn.bridge.snapshot()
		starts := r87carryStarts(frames)
		if got := r87carryTypes(frames, 0); !reflect.DeepEqual(got, []string{"resume", "start"}) || starts[0].Leg != 2 || accepted != 1 {
			t.Fatalf("notIdempotent=%v: frames = %+v, accepted = %d", notIdempotent, frames, accepted)
		}
	}
}

func TestR87carryQueuedStartBeforeUserMessages(t *testing.T) {
	for _, notIdempotent := range []bool{false, true} {
		turn := r87carryStartTurn(t, r87carryTurnOpts{startDuringReconnect: true, submitDuringReconnect: true, notIdempotent: notIdempotent})
		turn.mustFinish(t)
		frames, _ := turn.bridge.snapshot()
		types := r87carryTypes(frames, 0)
		if len(types) < 3 || !reflect.DeepEqual(types[:3], []string{"resume", "start", "user-message"}) || len(r87carryStarts(frames)) != 1 {
			t.Fatalf("notIdempotent=%v: frames = %q", notIdempotent, types)
		}
		for _, typ := range types {
			if typ == "abort" {
				t.Fatalf("notIdempotent=%v: an abort was sent: %q", notIdempotent, types)
			}
		}
	}
}

func TestR87carryRecoversStartLostAfterIdleReconnect(t *testing.T) {
	for _, notIdempotent := range []bool{false, true} {
		turn := r87carryStartTurn(t, r87carryTurnOpts{drop: true, startDuringReconnect: true, notIdempotent: notIdempotent})
		if !notIdempotent {
			turn.mustFinish(t)
			frames, accepted := turn.bridge.snapshot()
			starts := r87carryStarts(frames)
			if len(starts) != 2 || starts[0].Leg != 2 || starts[1].Leg != 3 || starts[1].StartID != starts[0].StartID || accepted != 1 {
				t.Fatalf("starts = %+v, accepted = %d", starts, accepted)
			}
			continue
		}
		err := turn.done(t)
		if err == nil || !strings.Contains(err.Error(), "does not support safe start retries") {
			t.Fatalf("err = %v", err)
		}
		frames, _ := turn.bridge.snapshot()
		if starts := r87carryStarts(frames); len(starts) != 1 || starts[0].Leg != 2 {
			t.Fatalf("starts = %+v", starts)
		}
	}
}

func TestR87carryDoesNotDuplicateAcceptedTurn(t *testing.T) {
	for _, finished := range []bool{false, true} {
		for _, notIdempotent := range []bool{false, true} {
			turn := r87carryStartTurn(t, r87carryTurnOpts{drop: true, acceptBeforeDrop: true, finishBeforeReconnect: finished, notIdempotent: notIdempotent})
			turn.mustFinish(t)
			// TS 5a05735ed0 (claude-code-start.integration.test.ts "does not
			// duplicate an accepted turn"): an idempotent bridge gets the
			// pending start again on reconnect (it drops the repeated
			// startId); an older one is not resent the accepted start. The
			// reconnect handlers run on the channel queue before the bridge's
			// resume-triggered finish is dispatched, so the resend always
			// happens; the fake bridge records it on its own goroutine, so
			// wait for the record before asserting the exact count.
			want := 2
			if notIdempotent {
				want = 1
			}
			frames, accepted := turn.bridge.snapshot()
			for deadline := time.Now().Add(2 * time.Second); len(r87carryStarts(frames)) < want && time.Now().Before(deadline); {
				time.Sleep(5 * time.Millisecond)
				frames, accepted = turn.bridge.snapshot()
			}
			starts := r87carryStarts(frames)
			ids := map[string]bool{}
			for _, s := range starts {
				ids[s.StartID] = true
			}
			if len(starts) != want || len(ids) != 1 || accepted != 1 {
				t.Fatalf("finished=%v notIdempotent=%v: starts = %+v, accepted = %d", finished, notIdempotent, starts, accepted)
			}
		}
	}
}

func TestR87carryOlderWaitingBridgeFails(t *testing.T) {
	turn := r87carryStartTurn(t, r87carryTurnOpts{drop: true, notIdempotent: true})
	err := turn.done(t)
	if err == nil || err.Error() != "claude-code bridge is waiting for a start after reconnect and does not support safe start retries." {
		t.Fatalf("err = %v", err)
	}
	frames, _ := turn.bridge.snapshot()
	if starts := r87carryStarts(frames); len(starts) != 1 {
		t.Fatalf("starts = %+v", starts)
	}
}

// TS claude-code-harness.test.ts (3bf70f83bc): the first-event deadline.
func r87carrySilentHarness(t *testing.T, timeout time.Duration, frames chan<- string) (harness.Session, *bridgetest.Server) {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey, OnFrame: func(typ string) {
		select {
		case frames <- typ:
		default:
		}
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second, TurnStartTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	return sess, srv
}

func r87carryHasFrame(frames <-chan string, want string, within time.Duration) bool {
	deadline := time.After(within)
	for {
		select {
		case typ := <-frames:
			if typ == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// TS: "fails a silent turn after the startup deadline".
func TestR87carryFailsSilentTurnAfterDeadline(t *testing.T) {
	frames := make(chan string, 16)
	sess, _ := r87carrySilentHarness(t, 100*time.Millisecond, frames)
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	r87carryWait(t, control.Done())
	if err := control.Err(); err == nil || err.Error() != "claude-code bridge did not emit a turn event within 100ms." {
		t.Fatalf("err = %v", err)
	}
	if !r87carryHasFrame(frames, "abort", 2*time.Second) {
		t.Fatal("no abort was sent")
	}
}

// TS: "clears the startup deadline on the first bridge event" and "clears
// the startup deadline when the caller aborts".
func TestR87carryDeadlineClearedByFirstEventOrAbort(t *testing.T) {
	t.Run("first event", func(t *testing.T) {
		frames := make(chan string, 16)
		srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey,
			OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
				turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "Hello"})
			},
			OnFrame: func(typ string) {
				select {
				case frames <- typ:
				default:
				}
			}})
		t.Cleanup(srv.Close)
		sandbox := newTestSandbox(srv)
		wireSpawn(sandbox)
		// The deadline leaves room for a loaded machine; the turn must still
		// be open well after it.
		h, _ := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second, TurnStartTimeout: 500 * time.Millisecond})
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
		got := make(chan struct{}, 1)
		control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: func(p harness.StreamPart) {
			if p.PartType() == harness.PartTypeTextDelta {
				got <- struct{}{}
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		r87carryWait(t, got)
		time.Sleep(700 * time.Millisecond)
		select {
		case <-control.Done():
			t.Fatalf("the turn settled after its first event: %v", control.Err())
		default:
		}
		if r87carryHasFrame(frames, "abort", 0) {
			t.Fatal("an abort was sent")
		}
	})
	t.Run("abort", func(t *testing.T) {
		frames := make(chan string, 16)
		sess, _ := r87carrySilentHarness(t, 100*time.Millisecond, frames)
		ctx, cancel := context.WithCancelCause(context.Background())
		control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: func(harness.StreamPart) {}})
		if err != nil {
			t.Fatal(err)
		}
		cause := &r87carryCancelled{}
		cancel(cause)
		r87carryWait(t, control.Done())
		if control.Err() != cause {
			t.Fatalf("err = %v, want the abort cause", control.Err())
		}
		time.Sleep(200 * time.Millisecond)
		if control.Err() != cause {
			t.Fatalf("the deadline replaced the abort cause: %v", control.Err())
		}
	})
}

type r87carryCancelled struct{}

func (*r87carryCancelled) Error() string { return "cancelled" }

// TS `turnStartTimeoutMs: -1`: Node runs the deadline after 1 ms, so the
// start is sent and the turn then fails with the configured value in the
// text (round 87e; Go used to disable the deadline for a negative value).
func TestR87carryNegativeTurnStartTimeoutFailsAtOnce(t *testing.T) {
	frames := make(chan string, 16)
	sess, _ := r87carrySilentHarness(t, -time.Millisecond, frames)
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	r87carryWait(t, control.Done())
	if err := control.Err(); err == nil || err.Error() != "claude-code bridge did not emit a turn event within -1ms." {
		t.Fatalf("err = %v", err)
	}
	if !r87carryHasFrame(frames, "start", 2*time.Second) {
		t.Fatal("the start was not sent before the deadline")
	}
	if !r87carryHasFrame(frames, "abort", 2*time.Second) {
		t.Fatal("no abort was sent")
	}
}
