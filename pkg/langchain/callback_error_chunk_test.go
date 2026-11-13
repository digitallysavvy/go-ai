package langchain

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func callbackErrorRun(t *testing.T, cb *StreamCallbacks) (types []string, errorText string, errs []error) {
	t.Helper()
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": "Hi"},
	}}
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}}}
	close(events)
	out, errCh := ToUIMessageStreamWithCallbacks(context.Background(), events, cb)
	for c := range out {
		types = append(types, c["type"].(string))
		if c["type"] == "error" {
			errorText, _ = c["errorText"].(string)
		}
	}
	for err := range errCh {
		errs = append(errs, err)
	}
	return types, errorText, errs
}

// TS wraps the whole stream body (including onToken, onFinal and onFinish) in
// the try block, so a throwing callback ends in the catch: onFinal, onError,
// then an `error` chunk; the stream closes normally.
func TestCallbackErrorsBecomeErrorChunk(t *testing.T) {
	boom := errors.New("callback failed")
	for _, c := range []struct {
		name      string
		cb        func(order *[]string) *StreamCallbacks
		wantOrder []string
	}{
		{"onToken", func(o *[]string) *StreamCallbacks {
			return &StreamCallbacks{
				OnToken: func(string) error { *o = append(*o, "token"); return boom },
				OnFinal: func(string) error { *o = append(*o, "final"); return nil },
				OnError: func(error) error { *o = append(*o, "error"); return nil },
			}
		}, []string{"token", "final", "error"}},
		{"onFinish", func(o *[]string) *StreamCallbacks {
			return &StreamCallbacks{
				OnFinal:  func(string) error { *o = append(*o, "final"); return nil },
				OnFinish: func(interface{}) error { *o = append(*o, "finish"); return boom },
				OnError:  func(error) error { *o = append(*o, "error"); return nil },
			}
		}, []string{"final", "finish", "final", "error"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var order []string
			types, text, errs := callbackErrorRun(t, c.cb(&order))
			if len(errs) != 0 {
				t.Fatalf("errors on the channel: %v", errs)
			}
			if text != "callback failed" || types[len(types)-1] != "error" {
				t.Fatalf("chunks %v, error text %q", types, text)
			}
			if !reflect.DeepEqual(order, c.wantOrder) {
				t.Fatalf("callback order %v, want %v", order, c.wantOrder)
			}
		})
	}
}

// A failure in the catch block's own callbacks (or onStart) errors the stream.
func TestCallbackErrorsInCatchBlockErrorTheStream(t *testing.T) {
	boom := errors.New("handler failed")
	_, _, errs := callbackErrorRun(t, &StreamCallbacks{
		OnToken: func(string) error { return errors.New("first") },
		OnError: func(error) error { return boom },
	})
	if len(errs) != 1 || errs[0] != boom {
		t.Fatalf("errs = %v", errs)
	}
	errored := false
	_, _, errs = callbackErrorRun(t, &StreamCallbacks{
		OnStart: func() error { return boom },
		OnError: func(error) error { errored = true; return nil },
	})
	if len(errs) != 1 || errs[0] != boom || errored {
		t.Fatalf("onStart: errs = %v, onError called = %v", errs, errored)
	}
}
