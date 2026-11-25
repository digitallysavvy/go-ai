package claudecode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS 'ignores an aborted turn tail while the next turn waits for its own
// answer': every event type of a turn that is no longer expected is dropped
// (no emit, no settle, no session id or finish metadata taken from it), and
// the expected turn's events then pass.
func TestPort2carryStaleTurnTailIsIgnored(t *testing.T) {
	s, conn := r88carrySession(t, 0)
	var mu sync.Mutex
	var got []string
	pc := s.wireTurn(context.Background(), func(p harness.StreamPart) {
		mu.Lock()
		got = append(got, p.PartType())
		mu.Unlock()
	}, "turn-b")
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
		{"type": "compaction", "phase": "start", "trigger": "auto", "summary": "s"},
		{"type": "raw", "rawValue": map[string]interface{}{"type": "system", "subtype": "init", "sessionId": "wrong-conversation"}},
		{"type": "finish-step", "finishReason": map[string]interface{}{"unified": "stop"}, "usage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}},
		{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}, "harnessMetadata": map[string]interface{}{"claude-code": map[string]interface{}{"sessionId": "wrong-conversation"}}},
		{"type": "error", "error": "old failure"},
	}
	for _, e := range stale {
		r88carryDispatch(conn, e, "turn-a")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("stale events reached the turn: %v", got)
	}
	mu.Unlock()
	select {
	case <-pc.done:
		t.Fatal("a stale event settled the turn")
	default:
	}
	if s.lastSessionID == "wrong-conversation" {
		t.Fatal("a stale event set the session id")
	}

	for _, e := range []map[string]interface{}{
		{"type": "stream-start"}, {"type": "text-start", "id": "new"}, {"type": "text-delta", "id": "new", "delta": "pineapple"},
		{"type": "text-end", "id": "new"}, {"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "totalUsage": map[string]interface{}{"inputTokens": map[string]interface{}{}, "outputTokens": map[string]interface{}{}}},
	} {
		r88carryDispatch(conn, e, "turn-b")
	}
	select {
	case <-pc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the expected turn did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"stream-start", "text-start", "text-delta", "text-end", "finish"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}
