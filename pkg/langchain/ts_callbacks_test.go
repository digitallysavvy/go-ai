package langchain

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Ports adapter.test.ts "toUIMessageStream callbacks" (the TS tests drive the
// callbacks through async generators that throw; Go carries the failure on
// StreamEvent.Err).
func runCallbacks(t *testing.T, events []StreamEvent, cb *StreamCallbacks) (chunkTypes []string, errorText string) {
	t.Helper()
	ch := make(chan StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	out, errs := ToUIMessageStreamWithCallbacks(context.Background(), ch, cb)
	for c := range out {
		chunkTypes = append(chunkTypes, c["type"].(string))
		if c["type"] == "error" {
			errorText, _ = c["errorText"].(string)
		}
	}
	<-errs
	return chunkTypes, errorText
}

func TestTSCallbacksOnFinishState(t *testing.T) {
	values := map[string]interface{}{"messages": []interface{}{map[string]interface{}{"id": "ai-1", "type": "ai", "content": "Hello!"}}}
	last := map[string]interface{}{"messages": []interface{}{map[string]interface{}{"id": "ai-1"}}, "step": 2}
	cases := []struct {
		name   string
		events []StreamEvent
		want   interface{}
	}{
		{"final state for LangGraph streams", []StreamEvent{{Mode: "values", Data: values}}, values},
		{"last values data when multiple values events", []StreamEvent{
			{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}, "step": 1}},
			{Mode: "values", Data: last},
		}, last},
		{"undefined for model streams", []StreamEvent{{Mode: "model", Data: map[string]interface{}{"type": "ai", "content": "Hello", "id": "test-1"}}}, nil},
		{"undefined for streamEvents streams", []StreamEvent{{Mode: "streamEvents", Data: map[string]interface{}{
			"event": "on_chat_model_stream",
			"data":  map[string]interface{}{"chunk": map[string]interface{}{"id": "msg-1", "content": "Hello"}},
		}}}, nil},
		{"[namespace, type, data] format for values events", []StreamEvent{
			ParseLangGraphEvent([]interface{}{"some-namespace", "values", values}),
		}, values},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			called := 0
			var got interface{}
			runCallbacks(t, c.events, &StreamCallbacks{OnFinish: func(s interface{}) error { called++; got = s; return nil }})
			if called != 1 || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("OnFinish called %d times with %#v, want once with %#v", called, got, c.want)
			}
		})
	}
}

func TestTSCallbacksErrorAndAbort(t *testing.T) {
	values := StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}}}
	t.Run("onError, no onFinish", func(t *testing.T) {
		var gotErr error
		finished := false
		types, text := runCallbacks(t, []StreamEvent{values, {Err: errors.New("Stream failed")}}, &StreamCallbacks{
			OnError:  func(err error) error { gotErr = err; return nil },
			OnFinish: func(interface{}) error { finished = true; return nil },
		})
		if gotErr == nil || gotErr.Error() != "Stream failed" || finished {
			t.Fatalf("onError=%v finished=%v", gotErr, finished)
		}
		if text != "Stream failed" || types[len(types)-1] != "error" {
			t.Fatalf("chunks %v, error text %q; want trailing error chunk", types, text)
		}
	})
	t.Run("onAbort, not onError or onFinish", func(t *testing.T) {
		aborted, errored, finished := false, false, false
		runCallbacks(t, []StreamEvent{values, {Err: context.Canceled}}, &StreamCallbacks{
			OnAbort:  func() error { aborted = true; return nil },
			OnError:  func(error) error { errored = true; return nil },
			OnFinish: func(interface{}) error { finished = true; return nil },
		})
		if !aborted || errored || finished {
			t.Fatalf("aborted=%v errored=%v finished=%v", aborted, errored, finished)
		}
	})
	t.Run("onFinal order", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			events []StreamEvent
			want   []string
		}{
			{"success", []StreamEvent{values}, []string{"onFinal", "onFinish"}},
			{"error", []StreamEvent{values, {Err: errors.New("Stream failed")}}, []string{"onFinal", "onError"}},
			{"abort", []StreamEvent{values, {Err: context.Canceled}}, []string{"onFinal", "onAbort"}},
		} {
			var order []string
			runCallbacks(t, c.events, &StreamCallbacks{
				OnFinal:  func(string) error { order = append(order, "onFinal"); return nil },
				OnFinish: func(interface{}) error { order = append(order, "onFinish"); return nil },
				OnError:  func(error) error { order = append(order, "onError"); return nil },
				OnAbort:  func() error { order = append(order, "onAbort"); return nil },
			})
			if !reflect.DeepEqual(order, c.want) {
				t.Fatalf("%s: order %v, want %v", c.name, order, c.want)
			}
		}
	})
}

func TestTSCallbacksStartFinalFinishOrderAndAccumulatedText(t *testing.T) {
	var order []string
	runCallbacks(t, []StreamEvent{{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}}}}, &StreamCallbacks{
		OnStart:  func() error { order = append(order, "start"); return nil },
		OnFinal:  func(string) error { order = append(order, "final"); return nil },
		OnFinish: func(interface{}) error { order = append(order, "finish"); return nil },
	})
	if !reflect.DeepEqual(order, []string{"start", "final", "finish"}) {
		t.Fatalf("order %v", order)
	}

	var final string
	chunk := func(text string) StreamEvent {
		return StreamEvent{Mode: "model", Data: map[string]interface{}{"type": "ai", "id": "msg-1", "content": text}}
	}
	runCallbacks(t, []StreamEvent{chunk("Hello"), chunk(" World"), {Err: errors.New("Mid-stream error")}}, &StreamCallbacks{
		OnFinal: func(s string) error { final = s; return nil },
	})
	if final != "Hello World" {
		t.Fatalf("onFinal got %q, want accumulated text on error", final)
	}
}
