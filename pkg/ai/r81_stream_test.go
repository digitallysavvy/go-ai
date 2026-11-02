package ai

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r81streamSharedCtxOnEnd runs StreamText and its UI conversion on one ctx,
// cancels it after the first text delta, and returns the OnEnd event.
func r81streamSharedCtxOnEnd(t *testing.T) map[string]interface{} {
	t.Helper()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		return &r79integBlockingStream{ctx: ctx, cancelled: make(chan struct{})}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]interface{}
	chunks, errCh := r.ToUIMessageStream(ctx, UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) { event = e }})
	for c := range chunks {
		if c["type"] == "text-delta" {
			cancel()
		}
	}
	<-errCh
	return event
}

// One ctx drives StreamText and ToUIMessageStream. Cancelling it after the
// first text delta always ends the UI conversion as a consumer cancellation
// (TS handleUIMessageStreamFinish cancel: isCancelled, outcome unknown): the
// abort part StreamText produces after the cancellation is never processed.
// Before, a read in flight when the ctx ended applied the abort part to the
// message state, so OnEnd reported isAborted with outcome aborted in a few
// runs out of a hundred.
func TestR81streamSharedCtxOnEndIsCancelled(t *testing.T) {
	for i := 0; i < 1000; i++ {
		e := r81streamSharedCtxOnEnd(t)
		got := fmt.Sprintf("isAborted=%v isCancelled=%v outcome=%v", e["isAborted"], e["isCancelled"], e["outcome"])
		if want := "isAborted=false isCancelled=true outcome=map[status:unknown]"; got != want {
			t.Fatalf("run %d: OnEnd %s, want %s", i, got, want)
		}
	}
}

// A MessageMetadata callback that cancels the UI ctx: the message-metadata
// chunk produced after the cancellation is neither written nor applied to
// the OnEnd message (TS: the cancelled pipeline processes no further chunk).
func TestR81streamNoStateAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeText, Text: "b"},
		{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
	})
	var event map[string]interface{}
	out, errCh := ToUIMessageStream(ctx, src, UIMessageStreamResultOptions{
		MessageMetadata: func(input map[string]interface{}) map[string]interface{} {
			if input["type"] == "text-delta" {
				cancel()
				return map[string]interface{}{"k": "v"}
			}
			return nil
		},
		OnEnd: func(e map[string]interface{}) { event = e },
	})
	var types []string
	for c := range out {
		types = append(types, c["type"].(string))
	}
	if err := <-errCh; err != context.Canceled {
		t.Fatalf("UI error = %v, want context.Canceled", err)
	}
	for _, typ := range types {
		if typ == "message-metadata" {
			t.Fatalf("chunks = %v: message-metadata written after cancel", types)
		}
	}
	msg := event["responseMessage"].(UIMessageChunk)
	if md, ok := msg["metadata"]; ok {
		t.Fatalf("responseMessage metadata = %v, want none (never written)", md)
	}
	if event["isCancelled"] != true {
		t.Fatalf("isCancelled = %v, want true", event["isCancelled"])
	}
}

// CreateUIMessageStreamWithOptions: a chunk written as the ctx is cancelled
// (here an abort chunk blocked in Write while the consumer is not reading) is
// not processed, so OnEnd reports the cancellation, never an abort.
func TestR81streamCreateUIMessageStreamCancelledChunkNotProcessed(t *testing.T) {
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		beforeAbort := make(chan struct{})
		var event map[string]interface{}
		out, errCh := CreateUIMessageStreamWithOptions(ctx, UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				w.Write(UIMessageChunk{"type": "start"})
				w.Write(UIMessageChunk{"type": "text-start", "id": "1"})
				close(beforeAbort)
				w.Write(UIMessageChunk{"type": "abort"})
			},
			OnEnd: func(e map[string]interface{}) { event = e },
		})
		if c := <-out; c["type"] != "start" {
			t.Fatalf("first chunk = %v", c)
		}
		<-beforeAbort
		time.Sleep(2 * time.Millisecond)
		cancel()
		for c := range out {
			if c["type"] == "abort" {
				t.Fatalf("run %d: abort chunk written after cancel", i)
			}
		}
		<-errCh
		if event["isAborted"] != false || event["isCancelled"] != true {
			t.Fatalf("run %d: isAborted = %v, isCancelled = %v, want false, true", i, event["isAborted"], event["isCancelled"])
		}
		if got := event["outcome"].(map[string]interface{})["status"]; got != "unknown" {
			t.Fatalf("run %d: outcome = %v, want unknown", i, got)
		}
	}
}

// ReadUIMessages with a cancelled ctx and chunks already waiting on the
// stream: no snapshot is written (the ctx wins over a ready chunk and over a
// ready reader). Before, Go's select picked between the ready chunk and the
// done ctx at random, so a cancelled consumer still got snapshots.
func TestR81streamReadUIMessagesCancelledCtxWins(t *testing.T) {
	for i := 0; i < 50; i++ {
		stream := make(chan UIMessageChunk, 8)
		stream <- UIMessageChunk{"type": "start", "messageId": "m1"}
		stream <- UIMessageChunk{"type": "text-start", "id": "t"}
		for j := 0; j < 5; j++ {
			stream <- UIMessageChunk{"type": "text-delta", "id": "t", "delta": "x"}
		}
		stream <- UIMessageChunk{"type": "text-end", "id": "t"}
		close(stream)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, errCh := ReadUIMessages(ctx, ReadUIMessagesOptions{Stream: stream})
		n := 0
		for range out {
			n++
		}
		if err := <-errCh; err != nil {
			t.Fatalf("err = %v", err)
		}
		if n != 0 {
			t.Fatalf("run %d: %d snapshots after the ctx was cancelled, want 0", i, n)
		}
	}
}
