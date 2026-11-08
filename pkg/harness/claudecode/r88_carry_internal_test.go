package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// r88carryConn is an in-memory bridge socket that records the frames the
// host sends.
type r88carryConn struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	sent   []map[string]interface{}
}

func newR88carryConn() *r88carryConn {
	return &r88carryConn{in: make(chan []byte, 64), closed: make(chan struct{})}
}

func (c *r88carryConn) Receive() ([]byte, error) {
	select {
	case m := <-c.in:
		return m, nil
	case <-c.closed:
		return nil, &bridge.CloseError{Code: 1000}
	}
}

func (c *r88carryConn) Send(b []byte) error {
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	c.mu.Lock()
	c.sent = append(c.sent, m)
	c.mu.Unlock()
	return nil
}

func (c *r88carryConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *r88carryConn) frames(typ string) []map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]interface{}
	for _, m := range c.sent {
		if m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func (c *r88carryConn) lastStart(t *testing.T) map[string]interface{} {
	t.Helper()
	starts := c.frames("start")
	if len(starts) == 0 {
		t.Fatal("no start sent")
	}
	return starts[len(starts)-1]
}

func r88carrySession(t *testing.T, timeout time.Duration) (*session, *r88carryConn) {
	t.Helper()
	conn := newR88carryConn()
	ch := bridge.NewChannel(bridge.ChannelOptions{Connect: func(context.Context) (bridge.Conn, error) { return conn, nil }})
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ch.Close)
	return newSession(sessionOptions{sessionID: "s1", channel: ch, turnStartTimeout: timeout, sandboxHomeDir: "/home/user", sandbox: bridgetest.NewSandbox()}), conn
}

var r88carrySeq float64

// r88carryDispatch sends one bridge event tagged with startID.
func r88carryDispatch(conn *r88carryConn, event map[string]interface{}, startID string) {
	r88carrySeq++
	event["seq"] = r88carrySeq
	if startID != "" {
		event["startId"] = startID
	}
	b, _ := json.Marshal(event)
	conn.in <- b
}

func r88carryWait(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not settle", what)
	}
}

// Ports TS 5a05735ed0 claude-code-harness.test.ts "ignores an aborted turn
// tail while the next turn waits for its own answer".
func TestR88carryIgnoresAbortedTurnTail(t *testing.T) {
	s, conn := r88carrySession(t, 0)
	ctx, cancel := context.WithCancelCause(context.Background())
	first, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Count."), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := conn.lastStart(t)["startId"].(string)
	cancel(errors.New("cancelled"))
	r88carryWait(t, first.Done(), "the aborted turn")
	if first.Err() == nil || first.Err().Error() != "cancelled" {
		t.Fatalf("first err = %v", first.Err())
	}

	var mu sync.Mutex
	var events []string
	second, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Answer pineapple."), Emit: func(p harness.StreamPart) {
		mu.Lock()
		events = append(events, p.PartType())
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	secondID, _ := conn.lastStart(t)["startId"].(string)
	if firstID == "" || secondID == "" || secondID == firstID {
		t.Fatalf("start ids %q / %q", firstID, secondID)
	}
	stale := []map[string]interface{}{
		{"type": "stream-start"},
		{"type": "text-start", "id": "old"},
		{"type": "text-delta", "id": "old", "delta": "two"},
		{"type": "text-end", "id": "old"},
		{"type": "reasoning-start", "id": "old-reasoning"},
		{"type": "reasoning-delta", "id": "old-reasoning", "delta": "old"},
		{"type": "reasoning-end", "id": "old-reasoning"},
		{"type": "tool-input-start", "id": "old-tool", "toolName": "bash"},
		{"type": "tool-input-delta", "id": "old-tool", "delta": "{}"},
		{"type": "tool-input-end", "id": "old-tool"},
		{"type": "tool-call", "toolCallId": "old-tool", "toolName": "bash", "input": "{}"},
		{"type": "tool-result", "toolCallId": "old-tool", "toolName": "bash", "result": "old"},
		{"type": "tool-approval-request", "toolCallId": "old-tool", "approvalId": "old-approval"},
		{"type": "compaction", "phase": "start", "trigger": "auto", "summary": "old"},
		{"type": "raw", "rawValue": "old"},
		{"type": "finish-step", "finishReason": map[string]interface{}{"unified": "stop"}, "usage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}},
		{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}, "harnessMetadata": map[string]interface{}{"claude-code": map[string]interface{}{"sessionId": "wrong-conversation"}}},
		{"type": "error", "error": "old failure"},
	}
	for _, ev := range stale {
		r88carryDispatch(conn, ev, firstID)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(events) != 0 {
		t.Fatalf("stale events reached the next turn: %v", events)
	}
	mu.Unlock()
	select {
	case <-second.Done():
		t.Fatalf("the next turn settled on a stale event: %v", second.Err())
	default:
	}

	for _, ev := range []map[string]interface{}{
		{"type": "stream-start"},
		{"type": "text-start", "id": "new"},
		{"type": "text-delta", "id": "new", "delta": "pineapple"},
		{"type": "text-end", "id": "new"},
		{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}},
	} {
		r88carryDispatch(conn, ev, secondID)
	}
	r88carryWait(t, second.Done(), "the next turn")
	if second.Err() != nil {
		t.Fatalf("second err = %v", second.Err())
	}
	mu.Lock()
	if strings.Join(events, ",") != "stream-start,text-start,text-delta,text-end,finish" {
		t.Fatalf("events = %v", events)
	}
	mu.Unlock()

	third, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Again."), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	start := conn.lastStart(t)
	if _, has := start["resumeSessionId"]; has {
		t.Fatalf("the stale finish's session id was adopted: %v", start)
	}
	r88carryDispatch(conn, map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}}, start["startId"].(string))
	r88carryWait(t, third.Done(), "the third turn")
}

// Ports TS 5a05735ed0 "does not clear the startup deadline for events from
// another turn".
func TestR88carryForeignEventsKeepStartDeadline(t *testing.T) {
	s, conn := r88carrySession(t, 100*time.Millisecond)
	control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hello"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	r88carryDispatch(conn, map[string]interface{}{"type": "stream-start"}, "previous-turn")
	r88carryDispatch(conn, map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}}, "previous-turn")
	r88carryWait(t, control.Done(), "the turn")
	if control.Err() == nil || !strings.Contains(control.Err().Error(), "did not emit a turn event within 100ms") {
		t.Fatalf("err = %v", control.Err())
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(conn.frames("abort")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no abort sent")
		}
		time.Sleep(time.Millisecond)
	}
}

// An event without a startId (an older bridge) belongs to the turn, and an
// unknown expected start adopts the first tagged event's id, which detach
// persists (TS `belongsToTurn`, coords `startId`).
func TestR88carryAdoptsFirstStartIDAndPersistsIt(t *testing.T) {
	s, conn := r88carrySession(t, 0)
	var mu sync.Mutex
	var got []string
	pc := s.wireTurn(context.Background(), func(p harness.StreamPart) {
		mu.Lock()
		got = append(got, p.PartType())
		mu.Unlock()
	}, "")
	r88carryDispatch(conn, map[string]interface{}{"type": "stream-start"}, "")
	r88carryDispatch(conn, map[string]interface{}{"type": "text-start", "id": "a"}, "turn-a")
	r88carryDispatch(conn, map[string]interface{}{"type": "text-start", "id": "b"}, "turn-b")
	r88carryDispatch(conn, map[string]interface{}{"type": "text-end", "id": "a"}, "turn-a")
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if strings.Join(got, ",") != "stream-start,text-start,text-end" {
		t.Fatalf("events = %v", got)
	}
	mu.Unlock()
	if data := s.resumeData(3); data.Bridge == nil || data.Bridge.StartID != "turn-a" {
		t.Fatalf("persisted coords = %+v", data.Bridge)
	}
	pc.settleSuccess()
}

// TS claude-code-bridge-protocol.test.ts: the outbound schema keeps a string
// startId on every turn event.
func TestR88carryDecodeOutboundStartID(t *testing.T) {
	msg, _, err := bridge.DecodeOutbound([]byte(`{"type":"stream-start","startId":"turn-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := msg.(bridge.StreamPartFrame); !ok || f.StartID != "turn-1" {
		t.Fatalf("frame = %#v", msg)
	}
	data, err := bridge.MarshalOutbound(msg)
	if err != nil || !strings.Contains(string(data), `"startId":"turn-1"`) {
		t.Fatalf("marshal = %s, %v", data, err)
	}
	msg, _, err = bridge.DecodeOutbound([]byte(`{"type":"stream-start"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := msg.(bridge.StreamPartFrame); f.StartID != "" {
		t.Fatalf("startId = %q", f.StartID)
	}
}

// A replayed / attached continuation keeps the persisted turn start (TS
// `startId: rerunContinue ? generateId() : activeStartId`, coords
// `startId` on replay and attach); a rerun gets a fresh one.
func TestR88carryContinuationKeepsPersistedStartID(t *testing.T) {
	if got := replayStartID(true, &bridgeCoords{StartID: "turn-1"}); got != "turn-1" {
		t.Fatalf("replay start = %q", got)
	}
	if got := replayStartID(false, &bridgeCoords{StartID: "turn-1"}); got != "" {
		t.Fatalf("rerun start = %q", got)
	}
	s, conn := r88carrySession(t, 0)
	s.activeStartID = "turn-1"
	var mu sync.Mutex
	var got []string
	control, err := s.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(p harness.StreamPart) {
		mu.Lock()
		got = append(got, p.PartType())
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	r88carryDispatch(conn, map[string]interface{}{"type": "text-start", "id": "x"}, "older-turn")
	r88carryDispatch(conn, map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}}, "turn-1")
	r88carryWait(t, control.Done(), "the continuation")
	mu.Lock()
	if strings.Join(got, ",") != "finish" {
		t.Fatalf("events = %v", got)
	}
	mu.Unlock()
	if len(conn.frames("start")) != 0 {
		t.Fatal("a replayed continuation sent a start")
	}

	s2, conn2 := r88carrySession(t, 0)
	s2.activeStartID = "turn-1"
	s2.rerunContinue = true
	if _, err := s2.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatal(err)
	}
	if id := conn2.lastStart(t)["startId"]; id == "turn-1" || id == "" || id == nil {
		t.Fatalf("rerun start id = %v", id)
	}
}

// TS 5a05735ed0 reconnect order: a bridge with idempotentStart always gets
// the pending start again, even when it reports a running turn or a later
// cursor; one without it is only checked while it waits.
func TestR88carryIdempotentBridgeAlwaysGetsPendingStart(t *testing.T) {
	yes := true
	for _, hello := range []*bridge.Hello{
		{State: "running", Capabilities: &bridge.HelloCapabilities{IdempotentStart: &yes}},
		{State: "waiting", LastSeq: r88carryFloat(99), Capabilities: &bridge.HelloCapabilities{IdempotentStart: &yes}},
	} {
		s, conn := r88carrySession(t, 0)
		h := hello
		s.opts.bridgeHello = func() *bridge.Hello { return h }
		control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hi"), Emit: func(harness.StreamPart) {}})
		if err != nil {
			t.Fatal(err)
		}
		pc := control.(*promptControl)
		pc.retryStartAfterReconnect()
		starts := conn.frames("start")
		if len(starts) != 2 || starts[0]["startId"] != starts[1]["startId"] {
			t.Fatalf("hello %+v: starts = %v", h, starts)
		}
		pc.settleSuccess()
	}
	// Without the capability a running bridge is left alone.
	s, conn := r88carrySession(t, 0)
	s.opts.bridgeHello = func() *bridge.Hello { return &bridge.Hello{State: "running"} }
	control, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	control.(*promptControl).retryStartAfterReconnect()
	if n := len(conn.frames("start")); n != 1 {
		t.Fatalf("starts = %d", n)
	}
}

func r88carryFloat(v float64) *float64 { return &v }

// TS a54f0fd8c6 claude-code-harness.test.ts "passes the host disconnect
// timeout to the bridge": the flag only for a set (positive) timeout.
func TestR88carryHostDisconnectTimeoutFlag(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{60 * time.Second, " --host-disconnect-timeout-ms '60000'"},
		{1500 * time.Microsecond, " --host-disconnect-timeout-ms '1.5'"},
		{0, ""},
		{-time.Second, ""},
	} {
		if got := hostDisconnectTimeoutFlag(tc.d); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.d, got, tc.want)
		}
	}
}
