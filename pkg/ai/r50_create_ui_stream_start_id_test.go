package ai

import (
	"context"
	"testing"
)

// TS handleUIMessageStreamFinish injects the up-front response message id
// into every start chunk whose messageId is null: a start chunk that carries
// its own id does not change what later start chunks get, and an explicit
// empty id is kept.
func TestR50CreateUIMessageStreamStartIDInjection(t *testing.T) {
	out, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		GenerateMessageID: func() string { return "generated" },
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "start", "messageId": "explicit"})
			w.Write(UIMessageChunk{"type": "start"})
			w.Write(UIMessageChunk{"type": "start", "messageId": ""})
		},
	})
	var ids []interface{}
	for c := range out {
		v, ok := c["messageId"]
		if !ok {
			v = "<absent>"
		}
		ids = append(ids, v)
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []interface{}{"explicit", "generated", ""}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}
