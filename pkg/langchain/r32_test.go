package langchain

import (
	"context"
	"testing"
)

// TS isAbortError matches name === 'AbortError' only. A deadline is a
// TimeoutError there, so toUIMessageStream reports it through onError (and the
// error chunk), not onAbort.
func TestR32DeadlineIsAnErrorNotAnAbort(t *testing.T) {
	values := StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}}}
	var aborted, errored bool
	types, text := runCallbacks(t, []StreamEvent{values, {Err: context.DeadlineExceeded}}, &StreamCallbacks{
		OnAbort: func() error { aborted = true; return nil },
		OnError: func(error) error { errored = true; return nil },
	})
	if aborted || !errored {
		t.Fatalf("aborted=%v errored=%v, want onError only", aborted, errored)
	}
	if types[len(types)-1] != "error" || text == "" {
		t.Fatalf("chunks %v, error text %q; want a trailing error chunk", types, text)
	}
}

func TestR32ContextDeadlineEndsStreamThroughOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	<-ctx.Done()
	events := make(chan StreamEvent)
	var aborted, errored bool
	chunks, errs := ToUIMessageStreamWithCallbacks(ctx, events, &StreamCallbacks{
		OnAbort: func() error { aborted = true; return nil },
		OnError: func(error) error { errored = true; return nil },
	})
	for range chunks {
	}
	for range errs {
	}
	if aborted || !errored {
		t.Fatalf("aborted=%v errored=%v, want onError only for a deadline", aborted, errored)
	}
}

// TS runs the values-event finalization (text-end/reasoning-end of the open
// messages) before it inspects the data, so a values event with null data
// still closes them, before the next message starts.
func TestR32ValuesEventWithNullDataStillClosesOpenParts(t *testing.T) {
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": "Hi"},
	}}
	events <- StreamEvent{Mode: "values", Data: nil}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-2", "type": "ai", "content": "There"},
	}}
	close(events)
	out, errCh := ToUIMessageStream(context.Background(), events)
	var order []string
	for c := range out {
		switch c["type"] {
		case "text-start", "text-end":
			order = append(order, c["type"].(string)+":"+c["id"].(string))
		}
	}
	for range errCh {
	}
	want := []string{"text-start:msg-1", "text-end:msg-1", "text-start:msg-2", "text-end:msg-2"}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// A values-event tool call whose OpenAI-format arguments are an object (not a
// JSON string) gets {} like TS (JSON.parse of an object throws).
func TestR32OpenAIToolCallArgumentsObjectFallsBackToEmpty(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{
		map[string]interface{}{"id": "m1", "type": "ai", "content": "", "additional_kwargs": map[string]interface{}{
			"tool_calls": []interface{}{map[string]interface{}{
				"id": "c1", "function": map[string]interface{}{"name": "lookup", "arguments": map[string]interface{}{"q": "x"}},
			}},
		}},
	}}}
	close(events)
	out, errCh := ToUIMessageStream(context.Background(), events)
	var input interface{}
	found := false
	for c := range out {
		if c["type"] == "tool-input-available" {
			input, found = c["input"], true
		}
	}
	for range errCh {
	}
	if !found {
		t.Fatal("no tool-input-available chunk")
	}
	m, ok := input.(map[string]interface{})
	if !ok || len(m) != 0 {
		t.Fatalf("input = %#v, want {}", input)
	}
}
