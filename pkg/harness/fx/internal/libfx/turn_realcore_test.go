package libfx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func bashHostTool(exec func(ctx context.Context, input json.RawMessage) (any, error)) Tool {
	return Tool{
		Name:        "bash",
		Description: "test",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
		Execute:     exec,
	}
}

func startTestAgent(t *testing.T, ctx context.Context, opts AgentOptions) *Agent {
	t.Helper()
	opts.Wasm = requireWasm(t)
	if opts.APIKey == "" {
		opts.APIKey = "test-key"
	}
	if opts.Model == "" {
		opts.Model = "anthropic/claude-haiku-4.5"
	}
	opts.Stderr = testWriter{t}
	a, err := StartAgent(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return a
}

func drainTurn(t *testing.T, ctx context.Context, turn *Turn) []Event {
	t.Helper()
	var events []Event
	for {
		ev, err := turn.Next(ctx)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		events = append(events, ev)
	}
}

func eventText(events []Event, typ string) string {
	var b strings.Builder
	for _, e := range events {
		if e.Type == typ {
			b.WriteString(e.Delta + e.Text)
		}
	}
	return b.String()
}

func TestAgentRealCoreTurnHooksAndCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := &fixtureFetch{t: t, fixtures: []string{"gateway-0.sse.txt", "gateway-1.sse.txt"}}
	var mu sync.Mutex
	var hooks []string
	var received []string
	a := startTestAgent(t, ctx, AgentOptions{
		Fetch: f.fetch,
		Tools: []Tool{bashHostTool(func(context.Context, json.RawMessage) (any, error) {
			return "LIBFX_FIXTURE", nil
		})},
		OnHook: func(e HookEvent) {
			mu.Lock()
			hooks = append(hooks, e.Type)
			mu.Unlock()
		},
		OnReceive: func(method string, _ json.RawMessage) {
			mu.Lock()
			received = append(received, method)
			mu.Unlock()
		},
	})
	turn, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drainTurn(t, ctx, turn)
	res, err := turn.Wait(ctx)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("result %+v, %v", res, err)
	}
	if got := eventText(events, "text_delta"); got != "READY" {
		t.Errorf("text = %q", got)
	}
	var starts, ends int
	for _, e := range events {
		switch e.Type {
		case "tool_start":
			starts++
			if e.Name != "bash" || e.ID != "toolu_01KswTwrpijqi88ffraLQVsY" {
				t.Errorf("tool_start %+v", e)
			}
		case "tool_end":
			ends++
			if e.IsError {
				t.Errorf("tool_end %+v", e)
			}
		}
	}
	if starts != 1 || ends != 1 {
		t.Errorf("tool events: %d starts, %d ends: %+v", starts, ends, events)
	}
	mu.Lock()
	modelEnds := 0
	for _, h := range hooks {
		if h == "model.end" {
			modelEnds++
		}
	}
	if modelEnds != 2 {
		t.Errorf("hooks = %v", hooks)
	}
	if len(received) == 0 {
		t.Error("no acp receive events")
	}
	mu.Unlock()
	if res.Usage["inputTokens"] <= 0 {
		t.Errorf("usage = %v", res.Usage)
	}
	cp, err := a.Checkpoint(ctx)
	if err != nil || len(cp) == 0 {
		t.Fatalf("checkpoint: %d bytes, %v", len(cp), err)
	}
	// Restoring the checkpoint into a new agent succeeds.
	b := startTestAgent(t, ctx, AgentOptions{Fetch: (&fixtureFetch{t: t}).fetch, Checkpoint: cp})
	if b.SessionID() == "" {
		t.Error("restored agent has no session")
	}
}

// TestAgentRealCoreSteering queues a steer while the core waits on a host
// tool; the core takes it at its next boundary and sends it with the next
// model request, and echoes it as a user message.
func TestAgentRealCoreSteering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := &fixtureFetch{t: t, fixtures: []string{"gateway-0.sse.txt", "gateway-1.sse.txt"}}
	var turnMu sync.Mutex
	var turn *Turn
	steerErr := make(chan error, 1)
	a := startTestAgent(t, ctx, AgentOptions{
		Fetch: f.fetch,
		Tools: []Tool{bashHostTool(func(context.Context, json.RawMessage) (any, error) {
			turnMu.Lock()
			tr := turn
			turnMu.Unlock()
			_, err := tr.Steer("After the tool, reply STEERED.")
			steerErr <- err
			return "LIBFX_FIXTURE", nil
		})},
	})
	turnMu.Lock()
	tr, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "test"}})
	turn = tr
	turnMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	events := drainTurn(t, ctx, tr)
	if _, err := tr.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-steerErr; err != nil {
		t.Fatalf("steer: %v", err)
	}
	if got := eventText(events, "user_message"); got != "After the tool, reply STEERED." {
		t.Errorf("steer echo = %q (events %+v)", got, events)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 2 || !strings.Contains(f.posts[1], "After the tool, reply STEERED.") {
		t.Errorf("second request does not carry the steer (%d posts)", len(f.posts))
	}
	if _, err := tr.Steer("late"); err == nil {
		t.Error("steering a finished turn succeeded")
	}
}

// blockingFetch serves a model stream that never ends until the request is
// aborted.
func blockingFetch(t *testing.T, started chan<- struct{}) FetchFunc {
	return func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return fixtureResponse(t, "gateway-models.json"), nil
		}
		pr, pw := io.Pipe()
		go func() {
			_, _ = pw.Write([]byte("data: {\"type\":\"stream-start\",\"warnings\":[]}\n\n"))
			select {
			case started <- struct{}{}:
			default:
			}
			<-req.Context().Done()
			_ = pw.CloseWithError(req.Context().Err())
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
	}
}

// TestAgentRealCoreCancelMidStream cancels a turn whose model stream is
// blocked; the core unwinds, the turn reports cancelled, and the agent can
// checkpoint and prompt again.
func TestAgentRealCoreCancelMidStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	fetch := blockingFetch(t, started)
	f := &fixtureFetch{t: t, fixtures: []string{"gateway-1.sse.txt"}}
	useFixture := false
	a := startTestAgent(t, ctx, AgentOptions{Fetch: func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		fixture := useFixture
		mu.Unlock()
		if fixture {
			return f.fetch(req)
		}
		return fetch(req)
	}})
	turnCtx, cancelTurn := context.WithCancel(ctx)
	turn, err := a.Prompt(turnCtx, []PromptBlock{{Type: "text", Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("model stream never started")
	}
	begin := time.Now()
	cancelTurn()
	res, err := turn.Wait(ctx)
	if err != nil || res.StopReason != "cancelled" {
		t.Fatalf("result %+v, %v", res, err)
	}
	if d := time.Since(begin); d > 10*time.Second {
		t.Errorf("cancel took %v", d)
	}
	if _, err := turn.Next(ctx); !errors.Is(err, io.EOF) {
		t.Errorf("next after cancel: %v", err)
	}
	if _, err := a.Checkpoint(ctx); err != nil {
		t.Fatalf("checkpoint after cancel: %v", err)
	}
	mu.Lock()
	useFixture = true
	mu.Unlock()
	again, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "again"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drainTurn(t, ctx, again)
	if res, err := again.Wait(ctx); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("second turn %+v, %v", res, err)
	}
	if got := eventText(events, "text_delta"); got != "READY" {
		t.Errorf("second turn text = %q", got)
	}
}

// longTextStream is a Gateway stream with n text deltas, enough to fill the
// turn's unread-event queue.
func longTextStream(n int) string {
	var b bytes.Buffer
	b.WriteString("data: {\"type\":\"stream-start\",\"warnings\":[]}\n\n")
	b.WriteString("data: {\"type\":\"text-start\",\"id\":\"0\"}\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "data: {\"type\":\"text-delta\",\"id\":\"0\",\"delta\":\"%c\"}\n\n", 'a'+i%26)
	}
	b.WriteString("data: {\"type\":\"text-end\",\"id\":\"0\"}\n\n")
	b.WriteString("data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\",\"raw\":\"end_turn\"},\"usage\":{\"inputTokens\":{\"total\":10},\"outputTokens\":{\"total\":600}}}\n\n")
	return b.String()
}

func longStreamFetch(t *testing.T, n int) FetchFunc {
	return func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return fixtureResponse(t, "gateway-models.json"), nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(longTextStream(n)))}, nil
	}
}

// TestAgentRealCoreBlockedConsumer leaves a turn's events unread: the core
// blocks in its stdout write once the unread limit is reached, resumes when
// the consumer reads, and loses nothing.
func TestAgentRealCoreBlockedConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const n = 1200
	a := startTestAgent(t, ctx, AgentOptions{Fetch: longStreamFetch(t, n)})
	turn, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-turn.Done():
		t.Fatal("the turn finished while its events were unread")
	case <-time.After(1500 * time.Millisecond):
	}
	turn.mu.Lock()
	queued := len(turn.queue)
	turn.mu.Unlock()
	if queued != maxUnreadEvents {
		t.Errorf("queued %d events while blocked, want %d", queued, maxUnreadEvents)
	}
	events := drainTurn(t, ctx, turn)
	if res, err := turn.Wait(ctx); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("result %+v, %v", res, err)
	}
	if got := len(eventText(events, "text_delta")); got != n {
		t.Errorf("text length %d, want %d", got, n)
	}
}

// TestAgentRealCoreCancelWhileConsumerBlocked cancels a turn whose core is
// blocked writing to a full event queue.
func TestAgentRealCoreCancelWhileConsumerBlocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a := startTestAgent(t, ctx, AgentOptions{Fetch: longStreamFetch(t, 1200)})
	turn, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		turn.mu.Lock()
		queued := len(turn.queue)
		turn.mu.Unlock()
		if queued == maxUnreadEvents {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue never filled (%d)", queued)
		}
		time.Sleep(20 * time.Millisecond)
	}
	turn.Cancel()
	res, err := turn.Wait(ctx)
	if err != nil || res.StopReason != "cancelled" {
		t.Fatalf("result %+v, %v", res, err)
	}
	if _, err := a.Checkpoint(ctx); err != nil {
		t.Fatalf("checkpoint after cancel: %v", err)
	}
}

func TestAgentRealCoreRichToolImage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const image = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	f := &fixtureFetch{t: t, fixtures: []string{"gateway-0.sse.txt", "gateway-1.sse.txt"}}
	a := startTestAgent(t, ctx, AgentOptions{
		Fetch: f.fetch,
		Tools: []Tool{bashHostTool(func(context.Context, json.RawMessage) (any, error) {
			return &TypedToolResult{Type: "libfx.tool-result", Text: "marker", Images: []ToolImage{{Type: "image", MimeType: "image/png", Data: image}}}, nil
		})},
	})
	turn, err := a.Prompt(ctx, []PromptBlock{{Type: "text", Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	drainTurn(t, ctx, turn)
	if _, err := turn.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 2 || !strings.Contains(f.posts[1], image) || !strings.Contains(f.posts[1], "image/png") {
		t.Errorf("second request does not carry the tool image")
	}
}
