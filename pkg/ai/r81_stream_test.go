package ai

import (
	"context"
	"errors"
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

// r81streamLateTranscription returns one delta part once its ctx ends, as a
// provider that hands back an already-buffered part after the cancellation,
// and then the ctx error.
type r81streamLateTranscription struct {
	ctx  context.Context
	sent bool
}

func (s *r81streamLateTranscription) Next() (*provider.TranscriptionStreamPart, error) {
	<-s.ctx.Done()
	if s.sent {
		return nil, s.ctx.Err()
	}
	s.sent = true
	return &provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "i", Delta: "late"}, nil
}
func (s *r81streamLateTranscription) Err() error   { return nil }
func (s *r81streamLateTranscription) Close() error { return nil }

type r81streamLateTranslation struct {
	ctx  context.Context
	sent bool
}

func (s *r81streamLateTranslation) Next() (*provider.SpeechTranslationStreamPart, error) {
	<-s.ctx.Done()
	if s.sent {
		return nil, s.ctx.Err()
	}
	s.sent = true
	return &provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, ID: "i", Delta: "late"}, nil
}
func (s *r81streamLateTranslation) Err() error   { return nil }
func (s *r81streamLateTranslation) Close() error { return nil }

// r81streamWaitNext calls next on a goroutine, calls cancel once it is
// blocked, and returns the part's delta ("" when none) and the error.
func r81streamWaitNext(t *testing.T, cancel func(), next func() (string, error)) (string, error) {
	t.Helper()
	type res struct {
		delta string
		err   error
	}
	ch := make(chan res, 1)
	go func() {
		d, err := next()
		ch <- res{d, err}
	}()
	time.Sleep(2 * time.Millisecond)
	cancel()
	select {
	case r := <-ch:
		return r.delta, r.err
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return after the cancellation")
		return "", nil
	}
}

// r81streamTranscriptionRun starts a transcription over the late provider
// and returns its FullStream and the caller's ctx cancel.
func r81streamTranscriptionRun(t *testing.T) (*StreamTranscriptionResult, interface {
	Next() (*TranscriptionStreamPart, error)
	Close() error
}, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	model := &mockTranscriptionStreamerModel{doStreamFn: func(ctx context.Context, _ *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
		return &provider.TranscriptionStreamResult{Stream: &r81streamLateTranscription{ctx: ctx}}, nil
	}}
	result, err := ExperimentalStreamTranscribe(ctx, StreamTranscribeOptions{Model: model, Audio: newMockChanAudioStream()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	return result, stream, cancel
}

// The caller's ctx is TS abortSignal, which goes only to the model: the
// pipe keeps writing what the model still returns, so a waiting reader gets
// the late part, then the ctx error. After FullStream.Close (TS pipeTo's
// signal) a waiting reader gets no part, only the cancel reason. Before,
// Go's select picked the ready reader or the done ctx at random in both.
func TestR81streamTranscriptionCancellationAndWaitingReader(t *testing.T) {
	for i := 0; i < 50; i++ {
		_, stream, cancel := r81streamTranscriptionRun(t)
		delta, err := r81streamWaitNext(t, cancel, func() (string, error) {
			p, err := stream.Next()
			if p != nil {
				return p.Delta, err
			}
			return "", err
		})
		if delta != "late" || err != nil {
			t.Fatalf("ctx run %d: delta = %q, err = %v; want the late part", i, delta, err)
		}
		if p, err := stream.Next(); p != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("ctx run %d: second Next = %v, %v; want context.Canceled", i, p, err)
		}

		_, stream, cancel = r81streamTranscriptionRun(t)
		delta, err = r81streamWaitNext(t, func() { _ = stream.Close() }, func() (string, error) {
			p, err := stream.Next()
			if p != nil {
				return p.Delta, err
			}
			return "", err
		})
		cancel()
		if delta != "" || err == nil || err.Error() != "Transcription stream was cancelled." {
			t.Fatalf("close run %d: delta = %q, err = %v; want no part and the cancel reason", i, delta, err)
		}
	}
}

func r81streamTranslationRun(t *testing.T, ctx context.Context, provide func(ctx context.Context) provider.SpeechTranslationStream) (*StreamTranslationResult, interface {
	Next() (*TranslationStreamPart, error)
	Close() error
}) {
	t.Helper()
	model := &mockSpeechTranslationModel{doStreamFn: func(ctx context.Context, _ *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
		return &provider.SpeechTranslationStreamResult{Stream: provide(ctx)}, nil
	}}
	result, err := ExperimentalStreamTranslate(ctx, StreamTranslateOptions{Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	return result, stream
}

// Translation, the sibling of transcription: the same waiting-reader rule,
// and the TS rejection reasons. After Close the accessors return
// "Translation stream was cancelled." also while the provider is still
// reading (v0.5 returned the provider's context error there); after the
// caller's ctx they return its cause also while a send was blocked (v0.5
// returned the Close text there).
func TestR81streamTranslationCancellation(t *testing.T) {
	late := func(ctx context.Context) provider.SpeechTranslationStream { return &r81streamLateTranslation{ctx: ctx} }
	next := func(stream interface {
		Next() (*TranslationStreamPart, error)
	}) func() (string, error) {
		return func() (string, error) {
			p, err := stream.Next()
			if p != nil {
				return p.Delta, err
			}
			return "", err
		}
	}
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		_, stream := r81streamTranslationRun(t, ctx, late)
		delta, err := r81streamWaitNext(t, cancel, next(stream))
		if delta != "late" || err != nil {
			t.Fatalf("ctx run %d: delta = %q, err = %v; want the late part", i, delta, err)
		}
		if p, err := stream.Next(); p != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("ctx run %d: second Next = %v, %v; want context.Canceled", i, p, err)
		}

		_, stream = r81streamTranslationRun(t, context.Background(), late)
		delta, err = r81streamWaitNext(t, func() { _ = stream.Close() }, next(stream))
		if delta != "" || err == nil || err.Error() != "Translation stream was cancelled." {
			t.Fatalf("close run %d: delta = %q, err = %v; want no part and the cancel reason", i, delta, err)
		}
	}

	// Close while the provider is reading (it returns its ctx error).
	blocking := func(ctx context.Context) provider.SpeechTranslationStream {
		return &liveProviderTranslationStream{ctx: ctx, parts: make(chan provider.SpeechTranslationStreamPart)}
	}
	result, stream := r81streamTranslationRun(t, context.Background(), blocking)
	time.Sleep(2 * time.Millisecond)
	_ = stream.Close()
	if _, err := result.TranslationText(); err == nil || err.Error() != "Translation stream was cancelled." {
		t.Fatalf("TranslationText after Close = %v, want the cancel reason", err)
	}

	// The caller's ctx while a send is blocked (nobody reads).
	cause := errors.New("caller stopped")
	cctx, ccancel := context.WithCancelCause(context.Background())
	parts := make(chan provider.SpeechTranslationStreamPart, 2)
	parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, ID: "i", Delta: "a"}
	parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, ID: "i", Delta: "b"}
	result, stream = r81streamTranslationRun(t, cctx, func(ctx context.Context) provider.SpeechTranslationStream {
		return &liveProviderTranslationStream{ctx: ctx, parts: parts}
	})
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	ccancel(cause)
	if _, err := result.TranslationText(); !errors.Is(err, cause) {
		t.Fatalf("TranslationText after the caller cancelled = %v, want the ctx cause", err)
	}
}

type r81streamLateSpeech struct{ ctx context.Context }

func (s *r81streamLateSpeech) Next() (*provider.SpeechStreamPart, error) {
	<-s.ctx.Done()
	p := audioPart("late")
	return &p, nil
}
func (s *r81streamLateSpeech) Err() error   { return nil }
func (s *r81streamLateSpeech) Close() error { return nil }

func TestR81streamSpeechCancelledCtxWinsOverReader(t *testing.T) {
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		model := &mockStreamingSpeechModel{doStream: func(ctx context.Context, _ *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
			return &provider.SpeechStreamResult{Stream: &r81streamLateSpeech{ctx: ctx}}, nil
		}}
		result, err := ExperimentalStreamSpeech(ctx, StreamSpeechOptions{Model: model, Text: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := result.FullStream()
		if err != nil {
			t.Fatal(err)
		}
		part, err := r81streamWaitNext(t, cancel, func() (string, error) {
			p, err := stream.Next()
			if p != nil {
				return string(p.Audio), err
			}
			return "", err
		})
		if part != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("run %d: part = %q, err = %v; want no part and context.Canceled", i, part, err)
		}
	}
}
