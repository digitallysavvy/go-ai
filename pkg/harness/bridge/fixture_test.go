package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

// Replays the NDJSON fixtures recorded from the real TS runBridge
// (testdata/runbridge/*.ndjson) through DecodeOutbound and a live Channel,
// proving the wire format and the reconnect/resume cursor logic against
// bytes the actual bridge produced (not just hand-written test fixtures).

// readNDJSONFrames reads path and returns each real frame line, skipping
// blank lines and the recorder's `{"_close":{...}}` annotations (not
// protocol frames — see record_runbridge.mts).
func readNDJSONFrames(t *testing.T, path string) []json.RawMessage {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var frames []json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Close json.RawMessage `json:"_close"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.Close != nil {
			continue // recorder annotation, not a frame
		}
		frames = append(frames, append(json.RawMessage(nil), line...))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return frames
}

func TestRunBridgeFixtureReadyAndUnauthorized(t *testing.T) {
	readyFrames := readNDJSONFrames(t, "testdata/runbridge/ready.ndjson")
	if len(readyFrames) != 1 {
		t.Fatalf("ready.ndjson: got %d frames, want 1", len(readyFrames))
	}
	ready, err := DecodeReady(readyFrames[0])
	if err != nil {
		t.Fatalf("DecodeReady: %v", err)
	}
	if ready.Port != 4319 {
		t.Fatalf("ready.Port = %d, want 4319", ready.Port)
	}

	// A rejected token never gets a bridge-hello or any other frame — the
	// fixture is only the recorder's close(1008) annotation.
	unauthorizedFrames := readNDJSONFrames(t, "testdata/runbridge/unauthorized.ndjson")
	if len(unauthorizedFrames) != 0 {
		t.Fatalf("unauthorized.ndjson: got %d real frames, want 0 (only the _close annotation)", len(unauthorizedFrames))
	}
}

func TestRunBridgeFixtureReplay(t *testing.T) {
	liveFrames := readNDJSONFrames(t, "testdata/runbridge/live.ndjson")
	resumeFrames := readNDJSONFrames(t, "testdata/runbridge/resume.ndjson")
	if len(liveFrames) < 2 || len(resumeFrames) < 2 {
		t.Fatalf("fixtures too short: live=%d resume=%d", len(liveFrames), len(resumeFrames))
	}

	// The first frame of each recording is the bridge-hello a real
	// Dial(WaitForHello: true) already consumes before Open()/reconnect
	// returns — it never reaches the Channel as an ordinary message.
	helloLive, liveFrames := liveFrames[0], liveFrames[1:]
	helloResume, resumeFrames := resumeFrames[0], resumeFrames[1:]
	for _, raw := range []json.RawMessage{helloLive, helloResume} {
		msg, seq, err := DecodeOutbound(raw)
		if err != nil {
			t.Fatalf("decode hello %s: %v", raw, err)
		}
		if _, ok := msg.(*Hello); !ok {
			t.Fatalf("expected *Hello, got %T", msg)
		}
		if seq != nil {
			t.Fatalf("bridge-hello must not carry a resume seq, got %v", *seq)
		}
	}

	// Every remaining recorded frame must decode cleanly on its own, ahead of
	// feeding it through a Channel.
	for _, raw := range append(append([]json.RawMessage{}, liveFrames...), resumeFrames...) {
		if _, _, err := DecodeOutbound(raw); err != nil {
			t.Fatalf("DecodeOutbound(%s): %v", raw, err)
		}
	}

	c := newConnector()
	ch := NewChannel(ChannelOptions{Connect: c.connect, Reconnect: testReconnect()})
	t.Cleanup(ch.Close)
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	conn1 := c.current()

	var mu sync.Mutex
	var order []string
	var umResponses []Event
	for _, typ := range []string{"stream-start", "text-start", "text-delta", "text-end", "finish", "user-message-response", "bridge-stop"} {
		typ := typ
		ch.On(typ, func(e Event) {
			mu.Lock()
			order = append(order, e.Type())
			if typ == "user-message-response" {
				umResponses = append(umResponses, e)
			}
			mu.Unlock()
		})
	}

	wantLive := []string{"stream-start", "text-start", "text-delta", "text-delta"}
	for _, raw := range liveFrames {
		conn1.deliverRaw(raw)
	}
	// Wait for both the handler-dispatched order to catch up and the cursor
	// to advance: dispatch() only enqueues an event before handleIncoming
	// updates LastSeenEventID, so delivery to listeners can lag behind the
	// cursor update when a concurrent selective-flush drain holds the
	// delivery lock. Waiting on LastSeenEventID alone (as this test used to)
	// let it read `order` before the last live frame's handler had run,
	// flaking under load; waiting for the expected event count too removes
	// that race, and the cursor check keeps the resume frame below
	// (lastSeenEventId:4) meaningful.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		return n >= len(wantLive) && ch.LastSeenEventID() == 4
	})

	mu.Lock()
	gotLive := append([]string(nil), order...)
	mu.Unlock()
	if !equalStrings(gotLive, wantLive) {
		t.Fatalf("order after live.ndjson = %v, want %v", gotLive, wantLive)
	}

	// Force a drop; the channel reconnects and asks the bridge to replay
	// everything past the persisted cursor.
	conn1.drop()
	waitFor(t, time.Second, func() bool { return c.count() == 2 })
	conn2 := c.current()
	waitFor(t, time.Second, func() bool { return len(conn2.Sent()) > 0 })
	wantResume := `{"type":"resume","lastSeenEventId":4}`
	if got := conn2.Sent(); len(got) == 0 || got[0] != wantResume {
		t.Fatalf("resume frame = %v, want first entry %q", got, wantResume)
	}

	wantOrder := []string{
		"stream-start", "text-start", "text-delta", "text-delta", // live.ndjson
		"text-delta", "text-delta", "user-message-response", "text-end", "finish", "user-message-response", "bridge-stop", // resume.ndjson
	}
	for _, raw := range resumeFrames {
		conn2.deliverRaw(raw)
	}
	// Same reasoning as the live.ndjson wait above: wait for the handler
	// order to reach its expected length, not just the cursor, so the
	// assertions below never read `order` while its last handler is still
	// dispatching.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		return n >= len(wantOrder) && ch.LastSeenEventID() == 8
	})

	mu.Lock()
	defer mu.Unlock()
	if !equalStrings(order, wantOrder) {
		t.Fatalf("final order = %v, want %v", order, wantOrder)
	}

	if len(umResponses) != 2 {
		t.Fatalf("user-message-response events = %d, want 2", len(umResponses))
	}
	accepted, ok := umResponses[0].Message.(*UserMessageResponse)
	if !ok || !accepted.Accepted || accepted.MessageID != "msg-1" {
		t.Fatalf("first user-message-response = %+v, want accepted msg-1", umResponses[0].Message)
	}
	if !umResponses[0].HasSeq || umResponses[0].Seq != 6 {
		t.Fatalf("accepted response Seq/HasSeq = %v/%v, want 6/true", umResponses[0].Seq, umResponses[0].HasSeq)
	}
	rejected, ok := umResponses[1].Message.(*UserMessageResponse)
	if !ok || rejected.Accepted || rejected.MessageID != "msg-2" || rejected.Error == nil {
		t.Fatalf("second user-message-response = %+v, want rejected msg-2 with an error", umResponses[1].Message)
	}
	if umResponses[1].HasSeq {
		t.Fatalf("rejected response must carry no seq, got %v", umResponses[1].Seq)
	}

	if ch.LastSeenEventID() != 8 {
		t.Fatalf("LastSeenEventID = %v, want 8 (the rejected response and bridge-stop carry no seq and must not advance it)", ch.LastSeenEventID())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
