package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// A writer.Merge called from another goroutine as the last merged stream
// ends (TS: merging while a merged stream is still open extends the stream;
// after the end the merge is dropped). Each run either forwards the late
// stream or drops it, and never misuses the stream's wait group (the race
// detector reports an Add at zero concurrent with Wait).
func TestR81streamLateMergeAtEndIsSafe(t *testing.T) {
	for i := 0; i < 300; i++ {
		late := make(chan UIMessageChunk, 1)
		late <- UIMessageChunk{"type": "text-start", "id": "late"}
		close(late)
		out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				first := make(chan UIMessageChunk)
				w.Merge(first)
				go func() {
					first <- UIMessageChunk{"type": "start"}
					close(first)
					for j := 0; j < i%7; j++ {
						runtime.Gosched()
					}
					w.Merge(late)
				}()
			},
		})
		var got []string
		for c := range out {
			got = append(got, c["type"].(string))
		}
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		// The two merged streams interleave in any order.
		hasStart := false
		for _, typ := range got {
			hasStart = hasStart || typ == "start"
		}
		if !hasStart || len(got) > 2 {
			t.Fatalf("run %d: chunks = %v", i, got)
		}
	}
}

// ChunksContext with a ctx that is already cancelled delivers nothing and
// leaves the shared cursor at the start: the ctx wins over a ready buffer
// slot (Go's select picked between them at random, so some chunks were taken
// from the cursor and sent to a consumer that had already cancelled).
func TestR81streamChunksContextCancelledCtxWins(t *testing.T) {
	for i := 0; i < 200; i++ {
		res := manyChunkResult(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		n := 0
		for range res.ChunksContext(ctx) {
			n++
		}
		if n != 0 {
			t.Fatalf("run %d: %d chunks after the ctx was cancelled, want 0", i, n)
		}
		text := 0
		st := res.Stream()
		for {
			c, err := st.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeText {
				text++
			}
		}
		if text != 100 {
			t.Fatalf("run %d: Stream() then read %d text chunks, want 100", i, text)
		}
	}
}

// TS ui-message-stream-cancellation.node.test.ts 207 (both orders): the
// StreamText abort and the UI reader's cancellation in the same tick. The
// UI helper has its own ctx (TS: toUIMessageStream takes no abort signal).
// OnEnd: isAborted false, isCancelled true, outcome unknown; onAbort fired.
func TestR81streamTSSameTickAbortAndCancel(t *testing.T) {
	for _, order := range []string{"abort-first", "cancel-first"} {
		for i := 0; i < 200; i++ {
			model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				return &r79integBlockingStream{ctx: ctx, cancelled: make(chan struct{})}, nil
			}}
			abortCtx, abort := context.WithCancel(context.Background())
			uiCtx, cancelUI := context.WithCancel(context.Background())
			defer abort()
			defer cancelUI()
			aborted := make(chan struct{})
			r, err := StreamText(abortCtx, StreamTextOptions{Model: model, Prompt: "Hello",
				OnAbortEvent: func(context.Context, GenerateTextAbortEvent) { close(aborted) }})
			if err != nil {
				t.Fatal(err)
			}
			var event map[string]interface{}
			chunks, errCh := r.ToUIMessageStream(uiCtx, UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) { event = e }})
			for c := range chunks {
				if c["type"] == "text-delta" {
					if order == "abort-first" {
						abort()
						cancelUI()
					} else {
						cancelUI()
						abort()
					}
				}
			}
			<-errCh
			select {
			case <-aborted:
			case <-time.After(2 * time.Second):
				t.Fatalf("%s run %d: onAbort not called", order, i)
			}
			got := fmt.Sprintf("isAborted=%v isCancelled=%v outcome=%v", event["isAborted"], event["isCancelled"], event["outcome"])
			if want := "isAborted=false isCancelled=true outcome=map[status:unknown]"; got != want {
				t.Fatalf("%s run %d: OnEnd %s, want %s", order, i, got, want)
			}
		}
	}
}

// r81streamDisconnectWriter fails the write that carries the text delta
// and aborts the StreamText ctx first (TS: response 'close' aborts the
// request and cancels the response reader in the same tick).
type r81streamDisconnectWriter struct{ abort func() }

func (w r81streamDisconnectWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "text-delta") {
		w.abort()
		return 0, errors.New("client disconnected")
	}
	return len(p), nil
}

// TS ui-message-stream-cancellation.node.test.ts 244: a client disconnect
// without consumeSseStream aborts the request and cancels the response in
// the same tick. OnEnd: isAborted false, isCancelled true, outcome unknown;
// onAbort fired. Go form: the handler passes the request ctx (cancelled on
// the disconnect) to both StreamText and the Pipe helper; Go has no "same
// tick" for two separate contexts.
func TestR81streamTSDisconnectReportsCancellation(t *testing.T) {
	for i := 0; i < 200; i++ {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return &r79integBlockingStream{ctx: ctx, cancelled: make(chan struct{})}, nil
		}}
		abortCtx, abort := context.WithCancel(context.Background())
		aborted := make(chan struct{})
		r, err := StreamText(abortCtx, StreamTextOptions{Model: model, Prompt: "Hello",
			OnAbortEvent: func(context.Context, GenerateTextAbortEvent) { close(aborted) }})
		if err != nil {
			t.Fatal(err)
		}
		ended := make(chan map[string]interface{}, 1)
		err = PipeUIMessageStreamToResponseWithInit(abortCtx, r, r81streamDisconnectWriter{abort: abort}, nil,
			UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) { ended <- e }})
		if err == nil || err.Error() != "client disconnected" {
			t.Fatalf("run %d: pipe error = %v", i, err)
		}
		var event map[string]interface{}
		select {
		case event = <-ended:
		case <-time.After(2 * time.Second):
			t.Fatalf("run %d: onEnd not called", i)
		}
		select {
		case <-aborted:
		case <-time.After(2 * time.Second):
			t.Fatalf("run %d: onAbort not called", i)
		}
		got := fmt.Sprintf("isAborted=%v isCancelled=%v outcome=%v", event["isAborted"], event["isCancelled"], event["outcome"])
		if want := "isAborted=false isCancelled=true outcome=map[status:unknown]"; got != want {
			t.Fatalf("run %d: OnEnd %s, want %s", i, got, want)
		}
	}
}

// r81streamFinishChunks parses a JSON array of UI chunks.
func r81streamFinishChunks(t *testing.T, s string) []UIMessageChunk {
	t.Helper()
	var chunks []UIMessageChunk
	if err := json.Unmarshal([]byte(s), &chunks); err != nil {
		t.Fatal(err)
	}
	return chunks
}

// r81streamFinishRun writes chunks through CreateUIMessageStreamWithOptions
// (TS handleUIMessageStreamFinish over createUIMessageStream) and returns the
// output as JSON.
func r81streamFinishRun(t *testing.T, chunks []UIMessageChunk, opts UIMessageStreamOptions) string {
	t.Helper()
	opts.Execute = func(w UIMessageStreamWriter) {
		for _, c := range chunks {
			w.Write(c)
		}
	}
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), opts)
	var got []UIMessageChunk
	for c := range out {
		got = append(got, c)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// r81streamCanon re-encodes a JSON array of chunks with sorted keys.
func r81streamCanon(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(r81streamFinishChunks(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func r81streamJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS handle-ui-message-stream-finish.test.ts 392, 456, 481, 703, 752, 820,
// 864, 903: pass-through and callback cases.
func TestR81streamTSHandleFinishPassThroughAndSteps(t *testing.T) {
	t.Run("392 abort reason", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-abort-reason"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Starting text"},{"type":"abort","reason":"manual abort"},{"type":"finish"}]`
		calls := 0
		got := r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{OnEnd: func(map[string]interface{}) { calls++ }})
		if got != r81streamCanon(t, in) || calls != 1 {
			t.Fatalf("got %s (onEnd %d), want %s once", got, calls, in)
		}
	})
	t.Run("456 abort pass-through without onEnd", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-abort-passthrough"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Text before abort"},{"type":"abort"},{"type":"finish"}]`
		errs := 0
		got := r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{OnError: func(error) string { errs++; return "" }})
		if got != r81streamCanon(t, in) || errs != 0 {
			t.Fatalf("got %s (onError %d), want %s", got, errs, in)
		}
	})
	t.Run("481 multiple aborts", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-multiple-abort"},{"type":"text-start","id":"text-1"},{"type":"abort"},{"type":"text-delta","id":"text-1","delta":"Some text"},{"type":"abort"},{"type":"finish"}]`
		var events []map[string]interface{}
		got := r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{OnEnd: func(e map[string]interface{}) { events = append(events, e) }})
		if got != r81streamCanon(t, in) || len(events) != 1 || events[0]["isAborted"] != true {
			t.Fatalf("got %s, events %v", got, events)
		}
	})
	t.Run("703 multiple steps", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-multi-step"},
			{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Step 1"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},
			{"type":"start-step"},{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"Step 2"},{"type":"text-end","id":"text-2"},{"type":"finish-step"},
			{"type":"start-step"},{"type":"text-start","id":"text-3"},{"type":"text-delta","id":"text-3","delta":"Step 3"},{"type":"text-end","id":"text-3"},{"type":"finish-step"},
			{"type":"finish"}]`
		var lengths []int
		r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{OnStepEnd: func(e map[string]interface{}) {
			lengths = append(lengths, len(uiParts(e["responseMessage"].(UIMessageChunk))))
		}})
		if fmt.Sprint(lengths) != "[1 3 5]" {
			t.Fatalf("step part counts = %v, want [1 3 5]", lengths)
		}
	})
	t.Run("752 both callbacks", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-both"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}]`
		steps, ends := 0, 0
		r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{
			OnStepEnd: func(map[string]interface{}) { steps++ },
			OnEnd:     func(map[string]interface{}) { ends++ },
		})
		if steps != 1 || ends != 1 {
			t.Fatalf("onStepEnd %d, onEnd %d, want 1 and 1", steps, ends)
		}
	})
	t.Run("820 continuation with onStepEnd", func(t *testing.T) {
		in := `[{"type":"start","messageId":"assistant-msg-1"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":" continued"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}]`
		original := r81streamFinishChunks(t, `[{"id":"user-msg-1","role":"user","parts":[{"type":"text","text":"Continue this"}]},{"id":"assistant-msg-1","role":"assistant","parts":[{"type":"text","text":"This is"}]}]`)
		var events []map[string]interface{}
		r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{
			OriginalMessages:  original,
			GenerateMessageID: func() string { return "msg-999" },
			OnStepEnd:         func(e map[string]interface{}) { events = append(events, e) },
		})
		if len(events) != 1 {
			t.Fatalf("onStepEnd calls = %d", len(events))
		}
		e := events[0]
		if e["isContinuation"] != true || e["responseMessage"].(UIMessageChunk)["id"] != "assistant-msg-1" || len(e["messages"].([]UIMessageChunk)) != 2 {
			t.Fatalf("step event = %s", r81streamJSON(t, e))
		}
	})
	t.Run("864 step messages are copies", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-clone"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}]`
		var stepParts, endParts int
		r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{
			OnStepEnd: func(e map[string]interface{}) {
				msg := e["responseMessage"].(UIMessageChunk)
				parts := uiParts(msg)
				if len(parts) > 0 {
					if p, ok := parts[0].(map[string]interface{}); ok {
						p["text"] = "MUTATION!"
					}
				}
				msg["parts"] = append(parts, map[string]interface{}{"type": "text", "text": "MUTATION!"})
				stepParts = len(uiParts(msg))
			},
			OnEnd: func(e map[string]interface{}) {
				msg := e["responseMessage"].(UIMessageChunk)
				endParts = len(uiParts(msg))
				if r81streamJSON(t, uiParts(msg)) != `[{"state":"done","text":"Hello","type":"text"}]` {
					t.Errorf("onEnd parts = %s", r81streamJSON(t, uiParts(msg)))
				}
			},
		})
		if stepParts != 2 || endParts != 1 {
			t.Fatalf("step parts %d, end parts %d, want 2 and 1", stepParts, endParts)
		}
	})
	t.Run("903 no callbacks", func(t *testing.T) {
		in := `[{"type":"start","messageId":"msg-passthrough"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Test"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}]`
		errs := 0
		got := r81streamFinishRun(t, r81streamFinishChunks(t, in), UIMessageStreamOptions{OnError: func(error) string { errs++; return "" }})
		if got != r81streamCanon(t, in) || errs != 0 {
			t.Fatalf("got %s (onError %d), want %s", got, errs, in)
		}
	})
}

// TS handle-ui-message-stream-finish.test.ts 546 (completed, failed,
// aborted) and 579: a declared outcome survives a reader cancellation
// (no isCancelled), and an abort chunk read before the cancellation wins.
func TestR81streamTSHandleFinishCancelOutcomes(t *testing.T) {
	run := func(t *testing.T, chunks []UIMessageChunk, declared *UIMessageStreamOutcome) map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var event map[string]interface{}
		out, errCh := CreateUIMessageStreamWithOptions(ctx, UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				for _, c := range chunks {
					w.Write(c)
				}
				if declared != nil {
					w.SetOutcome(*declared)
				}
				<-ctx.Done()
			},
			OnEnd: func(e map[string]interface{}) { event = e },
		})
		for range chunks {
			<-out
		}
		cancel()
		for range out {
		}
		<-errCh
		return event
	}
	failErr := errors.New("stream failed")
	for _, declared := range []UIMessageStreamOutcome{
		{Status: UIMessageStreamOutcomeCompleted},
		{Status: UIMessageStreamOutcomeFailed, Error: failErr},
		{Status: UIMessageStreamOutcomeAborted},
	} {
		t.Run("546 "+string(declared.Status), func(t *testing.T) {
			d := declared
			e := run(t, []UIMessageChunk{{"type": "start", "messageId": "msg-1"}}, &d)
			if _, ok := e["isCancelled"]; ok {
				t.Fatalf("isCancelled set: %v", e)
			}
			want := uiMessageStreamOutcomeMap(declared)
			if fmt.Sprint(e["outcome"]) != fmt.Sprint(want) {
				t.Fatalf("outcome = %v, want %v", e["outcome"], want)
			}
		})
	}
	t.Run("579 observed abort", func(t *testing.T) {
		e := run(t, []UIMessageChunk{{"type": "start", "messageId": "msg-1"}, {"type": "abort"}}, nil)
		if _, ok := e["isCancelled"]; ok || e["isAborted"] != true || fmt.Sprint(e["outcome"]) != "map[status:aborted]" {
			t.Fatalf("event: isAborted %v, isCancelled %v, outcome %v", e["isAborted"], e["isCancelled"], e["outcome"])
		}
	})
}

// TS to-ui-message-stream.test.ts 634: a source that fails after its finish
// part ends failed with the source error (the earlier finish does not hide
// it), and onEnd runs once. Go form: a StreamText-shaped source (start
// first), and the errored source writes the masked error chunk (owner
// ruling 2026-10-29, known difference).
func TestR81streamTSFinishThenSourceFailure(t *testing.T) {
	sourceErr := errors.New("source stream failed after finish")
	var ends r71streamCalls
	src := &r71streamPartsThenErr{parts: []provider.StreamChunk{
		{Type: provider.ChunkTypeStart},
		{Type: provider.ChunkTypeFinish, FinishReason: "stop", RawFinishReason: "stop"},
	}, err: sourceErr}
	got, err := r71streamCollectFrom(t, context.Background(), src, UIMessageStreamResultOptions{OnEnd: ends.record})
	if !errors.Is(err, sourceErr) {
		t.Fatalf("err = %v, want %v", err, sourceErr)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"start"},{"type":"finish","finishReason":"stop"},{"type":"error","errorText":"An error occurred."}]`)
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome := events[0]["outcome"].(map[string]interface{})
	if len(outcome) != 2 || outcome["status"] != "failed" || outcome["error"] != sourceErr {
		t.Fatalf("outcome = %v", outcome)
	}
}

// TS create-ui-message-stream-response.test.ts 129 and 305: an error chunk
// is written as a data event before [DONE], and a ConsumeSSEStream callback
// that fails (Go: returns an error; TS: an async callback that rejects) does
// not affect the response.
// Keys are sorted (known difference "UI message stream JSON key order").
func TestR81streamTSChunksResponseErrorsAndConsumeFailure(t *testing.T) {
	read := func(t *testing.T, chunks []UIMessageChunk, init *UIMessageStreamResponseInit) string {
		t.Helper()
		ch := make(chan UIMessageChunk, len(chunks))
		for _, c := range chunks {
			ch <- c
		}
		close(ch)
		resp, err := CreateUIMessageChunksResponse(ch, init)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		return string(b)
	}
	if got, want := read(t, []UIMessageChunk{{"type": "error", "errorText": "Custom error message"}}, &UIMessageStreamResponseInit{Status: 200}),
		"data: {\"errorText\":\"Custom error message\",\"type\":\"error\"}\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("129 body = %q, want %q", got, want)
	}
	for name, consume := range map[string]func(io.Reader) error{
		"error": func(io.Reader) error { return errors.New("consumeSseStream error") },
	} {
		calls := make(chan struct{}, 2)
		got := read(t, []UIMessageChunk{{"type": "text-delta", "id": "1", "delta": "error-test"}}, &UIMessageStreamResponseInit{Status: 200,
			ConsumeSSEStream: func(r io.Reader) error { calls <- struct{}{}; return consume(r) }})
		if want := "data: {\"delta\":\"error-test\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"; got != want {
			t.Fatalf("305 %s body = %q, want %q", name, got, want)
		}
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("305 %s: consumeSseStream not called", name)
		}
		if len(calls) != 0 {
			t.Fatalf("305 %s: consumeSseStream called more than once", name)
		}
	}
}

// TS create-ui-message-stream.test.ts 153: direct writes and two merged
// streams. The TS snapshot's interleaving follows JS microtask order; Go
// merges on goroutines, so the port checks what holds in both: every chunk
// arrives once, and each source keeps its own order.
func TestR81streamTSMergeMultipleStreams(t *testing.T) {
	delta := func(id, d string) UIMessageChunk { return UIMessageChunk{"type": "text-delta", "id": id, "delta": d} }
	s1 := make(chan UIMessageChunk, 5)
	s2 := make(chan UIMessageChunk, 2)
	executed := make(chan struct{})
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(delta("1", "data-part-1"))
			w.Merge(s1)
			s1 <- delta("1", "1a")
			w.Write(delta("1", "data-part-2"))
			s1 <- delta("1", "1b")
			w.Merge(s2)
			w.Write(delta("1", "data-part-3"))
			close(executed)
		},
	})
	go func() {
		// TS: the test enqueues after createUIMessageStream returns, and
		// execute runs synchronously; Go's Execute runs on its own goroutine.
		<-executed
		s2 <- delta("2", "2a")
		s1 <- delta("1", "1c")
		s2 <- delta("2", "2b")
		close(s2)
		s1 <- delta("1", "1d")
		s1 <- delta("1", "1e")
		close(s1)
	}()
	bySource := map[string][]string{}
	n := 0
	for c := range out {
		n++
		d := c["delta"].(string)
		src := "direct"
		switch d {
		case "1a", "1b", "1c", "1d", "1e":
			src = "s1"
		case "2a", "2b":
			src = "s2"
		}
		bySource[src] = append(bySource[src], d)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if n != 10 || fmt.Sprint(bySource) != "map[direct:[data-part-1 data-part-2 data-part-3] s1:[1a 1b 1c 1d 1e] s2:[2a 2b]]" {
		t.Fatalf("got %d chunks: %v", n, bySource)
	}
}

// TS get-response-ui-message-id.test.ts 8, 16, 28, 40, 48.
func TestR81streamTSGetResponseUIMessageID(t *testing.T) {
	gen := func() string { return "new-id" }
	msgs := func(roles ...string) []UIMessageChunk {
		out := []UIMessageChunk{}
		for i, r := range roles {
			out = append(out, UIMessageChunk{"id": fmt.Sprintf("msg-%d", i+1), "role": r, "parts": []interface{}{}})
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		opts   UIMessageStreamResultOptions
		want   string
		wantOK bool
	}{
		{"8 no original messages", UIMessageStreamResultOptions{GenerateMessageID: gen}, "", false},
		{"16 last assistant", UIMessageStreamResultOptions{OriginalMessages: msgs("user", "assistant"), GenerateMessageID: gen}, "msg-2", true},
		{"28 last user", UIMessageStreamResultOptions{OriginalMessages: msgs("assistant", "user"), GenerateMessageID: gen}, "new-id", true},
		{"40 empty", UIMessageStreamResultOptions{OriginalMessages: msgs(), GenerateMessageID: gen}, "new-id", true},
		{"48 string id", UIMessageStreamResultOptions{OriginalMessages: msgs(), ResponseMessageID: "response-id"}, "response-id", true},
	} {
		got, ok := responseUIMessageIDOK(tc.opts)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TS to-ui-message-chunk.test.ts 212: custom and lifecycle parts.
func TestR81streamTSToUIMessageChunkCustomAndLifecycle(t *testing.T) {
	meta := json.RawMessage(`{"testProvider":{"signature":"sig-1"}}`)
	metadata := func(map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"model": "test-model"}
	}
	f := false
	for _, tc := range []struct {
		name string
		part provider.StreamChunk
		opts UIMessageStreamResultOptions
		want string // "" = undefined
	}{
		{"custom", provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "openai.compaction"}, ProviderMetadata: meta}, UIMessageStreamResultOptions{},
			`{"kind":"openai.compaction","providerMetadata":{"testProvider":{"signature":"sig-1"}},"type":"custom"}`},
		{"start", provider.StreamChunk{Type: provider.ChunkTypeStart}, UIMessageStreamResultOptions{MessageMetadata: metadata, ResponseMessageID: "msg-1"},
			`{"messageId":"msg-1","messageMetadata":{"model":"test-model"},"type":"start"}`},
		{"start sendStart false", provider.StreamChunk{Type: provider.ChunkTypeStart}, UIMessageStreamResultOptions{SendStart: &f}, ""},
		{"finish", provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop", RawFinishReason: "stop"}, UIMessageStreamResultOptions{MessageMetadata: metadata},
			`{"finishReason":"stop","messageMetadata":{"model":"test-model"},"type":"finish"}`},
		{"finish sendFinish false", provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}, UIMessageStreamResultOptions{SendFinish: &f}, ""},
		{"start-step", provider.StreamChunk{Type: provider.ChunkTypeStartStep}, UIMessageStreamResultOptions{}, `{"type":"start-step"}`},
		{"finish-step", provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: "stop", RawFinishReason: "stop", ProviderMetadata: meta}, UIMessageStreamResultOptions{}, `{"type":"finish-step"}`},
		{"abort", provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user"}, UIMessageStreamResultOptions{}, `{"reason":"user","type":"abort"}`},
	} {
		chunk, ok := ToUIMessageChunk(tc.part, tc.opts)
		if tc.want == "" {
			if ok || chunk != nil {
				t.Errorf("%s: got %v, want undefined", tc.name, chunk)
			}
			continue
		}
		if got := r81streamJSON(t, chunk); !ok || got != tc.want {
			t.Errorf("%s: got %s (%v), want %s", tc.name, got, ok, tc.want)
		}
	}
}

// TS to-ui-message-chunk.test.ts 475 (schema input of a transformed
// approval request) and 679 (parts without a UI chunk).
func TestR81streamTSToUIMessageChunkApprovalSchemaInputAndUndefined(t *testing.T) {
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
		ApprovalID:       "approval-1",
		ToolCall:         types.ToolCall{ID: "call-1", ToolName: "staticTool", Arguments: map[string]interface{}{"value": "trimmed"}},
		InputSchemaInput: map[string]interface{}{"value": " trimmed "},
	}}, UIMessageStreamResultOptions{})
	if got, want := r81streamJSON(t, chunk), `{"approvalId":"approval-1","inputSchemaInput":{"value":" trimmed "},"toolCallId":"call-1","type":"tool-approval-request"}`; !ok || got != want {
		t.Fatalf("475: got %s (%v), want %s", got, ok, want)
	}
	for _, part := range []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
		{Type: provider.ChunkTypeRaw, Raw: json.RawMessage(`{"provider":"raw"}`)},
	} {
		if chunk, ok := ToUIMessageChunk(part, UIMessageStreamResultOptions{}); ok || chunk != nil {
			t.Errorf("679 %s: got %v, want undefined", part.Type, chunk)
		}
	}
}

// TS read-ui-message-stream.test.ts 259: a hydrated input-streaming static
// tool part without raw input accepts a later input delta and the final
// tool-input-available.
func TestR81streamTSReadUIMessagesHydratedPartialToolCall(t *testing.T) {
	var message UIMessage
	if err := json.Unmarshal([]byte(`{"id":"msg-123","role":"assistant","parts":[{"type":"tool-createDocument","toolCallId":"tool-1","state":"input-streaming","input":{"title":"Hel"}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	stream := make(chan UIMessageChunk, 2)
	stream <- UIMessageChunk{"type": "tool-input-delta", "toolCallId": "tool-1", "inputTextDelta": `lo"}`}
	stream <- UIMessageChunk{"type": "tool-input-available", "toolCallId": "tool-1", "toolName": "createDocument", "input": map[string]interface{}{"title": "Hello"}}
	close(stream)
	out, errCh := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Message: &message, Stream: stream, TerminateOnError: true})
	var last UIMessage
	n := 0
	for m := range out {
		last = m
		n++
	}
	if err := <-errCh; err != nil {
		t.Fatalf("err = %v", err)
	}
	if n == 0 {
		t.Fatal("no snapshots")
	}
	b, err := json.Marshal(last.Parts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `[{"type":"tool-createDocument","toolCallId":"tool-1","state":"input-available","input":{"title":"Hello"}}]`; got != want {
		t.Fatalf("parts = %s, want %s", got, want)
	}
}

const (
	r81streamCodeExecID  = "srvtoolu_01MzSrFWsmzBdcoQkGWLyRjK"
	r81streamContainerID = "container_011CWHPPTDTn1XufeRB9uHeH"
	r81streamDiceIntro   = "I'll help you simulate this game between two players where one is using a loaded die."
	r81streamDiceStdout  = "=== DICE GAME: First to 3 Wins ===\n\n--- Round 1 ---\nPlayer 1 rolls: 5\nPlayer 2 rolls: 6\nPlayer 2 wins this round!\n"
	r81streamDiceFinal   = "## Game Results\n\n**Player 2 wins the game 3-2!** 🏆"
)

// The stream fixture's rollDie calls, one per step for steps 1-4.
var r81streamDiceRolls = [][2]string{
	{"toolu_019jKkXz4jAdwHweHBw92CVY", "player1"},
	{"toolu_015dGLMbwBKv1ZRQr6KdJzeH", "player2"},
	{"toolu_01YYqBNq5mk1wMtv3PAqY44m", "player1"},
	{"toolu_018WxjDkQG8h7i63poySGT2x", "player2"},
}

// r81streamDiceSteps is the five model streams of stream-text.test.ts
// 'programmatic tool calling' > 'dice game fixture'. The stdout and final
// text are shortened (the assertions only compare them whole or check
// "Game Results"); the code_execution input is {"code": ...}, as in the gen
// port (the TS zod schema strips the extra "type" key).
func r81streamDiceSteps() [][]provider.StreamChunk {
	i64 := func(v int64) *int64 { return &v }
	use := func(in, out int64) *types.Usage {
		return &types.Usage{InputTokens: i64(in), InputDetails: &types.InputTokenDetails{NoCacheTokens: i64(in)}, OutputTokens: i64(out), OutputDetails: &types.OutputTokenDetails{TextTokens: i64(out)}, TotalTokens: i64(in + out)}
	}
	meta := json.RawMessage(`{"anthropic":{"container":{"id":"` + r81streamContainerID + `"}}}`)
	ids := []string{"msg_01ERcBqAvLTHWQDk9c9qJLWC", "msg_01KSVw3xmXbMNJPNMt46BC5W", "msg_016fLapHzDx8DG2SUcsGKyPA", "msg_01MQHz6AzmwmZoTry5nk5EQC", "msg_01CfmDducyrt61n4Q7QS8VFK"}
	rm := func(n int) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: ids[n], ModelID: "claude-sonnet-4-5-20250929", Timestamp: time.UnixMilli(int64(n) * 1000)}}
	}
	var steps [][]provider.StreamChunk
	for n, roll := range r81streamDiceRolls {
		step := []provider.StreamChunk{rm(n)}
		usage := use(0, 0)
		if n == 0 {
			usage = use(3369, 725)
			step = append(step,
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: r81streamDiceIntro},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: r81streamCodeExecID, ToolName: "code_execution", Arguments: map[string]interface{}{"code": "game_loop()"}, RawArguments: `{"code":"game_loop()"}`, ProviderExecuted: true}})
		}
		raw := `{ "player": "` + roll[1] + `" }`
		step = append(step,
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: roll[0], ToolName: "rollDie", Arguments: map[string]interface{}{"player": roll[1]}, RawArguments: raw}},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage, ProviderMetadata: meta})
		steps = append(steps, step)
	}
	steps = append(steps, []provider.StreamChunk{
		rm(4),
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: r81streamCodeExecID, ToolName: "code_execution", ProviderExecuted: true,
			Result: map[string]interface{}{"type": "code_execution_result", "stdout": r81streamDiceStdout, "stderr": "", "return_code": float64(0), "content": []interface{}{}}}},
		{Type: provider.ChunkTypeTextStart, ID: "2"},
		{Type: provider.ChunkTypeText, ID: "2", Text: r81streamDiceFinal},
		{Type: provider.ChunkTypeTextEnd, ID: "2"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: use(4551, 197)},
	})
	return steps
}

// stream-text.test.ts 'programmatic tool calling' > '5 steps: code_execution
// triggers client tool across multiple turns (dice game fixture)': step
// inputs (tools, prompt growth, container forwarding), toolCalls /
// toolResults, tool execution, steps, text, finishReason, totalUsage,
// prepareStep calls, onStepFinish and onFinish.
func TestR81streamTSProgrammaticToolCallingDiceGame(t *testing.T) {
	steps := r81streamDiceSteps()
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, o)
			return testutil.NewMockTextStream(steps[len(calls)-1]), nil
		}}
	type prepareCall struct {
		modelID    string
		stepNumber int
		steps      int
		messages   int
	}
	var prepareCalls []prepareCall
	var rolls []string
	var stepEvents []OnStepFinishEvent
	var endEvent OnFinishEvent
	rollDieOptions := map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": []interface{}{"code_execution_20250825"}}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true,
				Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string"}}, "required": []interface{}{"code"}}},
			{Name: "rollDie", Description: "Roll a die and return the result.",
				Parameters:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{"player": map[string]interface{}{"type": "string", "enum": []interface{}{"player1", "player2"}}}, "required": []interface{}{"player"}, "additionalProperties": false},
				ProviderOptions: rollDieOptions,
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					p, _ := in["player"].(string)
					mu.Lock()
					rolls = append(rolls, p)
					mu.Unlock()
					if p == "player1" {
						return 6, nil
					}
					return 3, nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepareCalls = append(prepareCalls, prepareCall{modelID: s.Model.ModelID(), stepNumber: s.StepNumber, steps: len(s.Steps), messages: len(s.Messages)})
			mu.Unlock()
			if s.StepNumber > 0 && len(s.Steps) > 0 {
				last := s.Steps[len(s.Steps)-1]
				if a, ok := last.ProviderMetadata["anthropic"].(map[string]interface{}); ok {
					if c, ok := a["container"].(map[string]interface{}); ok && c["id"] != nil {
						s.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": c["id"]}}}
					}
				}
			}
			return s
		},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { stepEvents = append(stepEvents, e) },
		OnEndEvent:     func(_ context.Context, e OnFinishEvent) { endEvent = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}

	// step inputs
	if len(calls) != 5 {
		t.Fatalf("model calls = %d, want 5", len(calls))
	}
	tools := calls[0].Tools
	if len(tools) != 2 || tools[0].Name != "code_execution" || !tools[0].ProviderExecuted || tools[1].Name != "rollDie" ||
		tools[1].Description != "Roll a die and return the result." || r81streamJSON(t, tools[1].ProviderOptions) != r81streamJSON(t, rollDieOptions) {
		t.Errorf("step 1 tools = %+v", tools)
	}
	wantOpts := `{"anthropic":{"container":{"id":"` + r81streamContainerID + `"}}}`
	if calls[0].ProviderOptions != nil {
		t.Errorf("step 1 providerOptions = %v, want none", calls[0].ProviderOptions)
	}
	for n := 1; n < 5; n++ {
		if got := r81streamJSON(t, calls[n].ProviderOptions); got != wantOpts {
			t.Errorf("step %d providerOptions = %s, want %s", n+1, got, wantOpts)
		}
		// Each step's prompt is the user message plus 2 messages per
		// earlier step (assistant, tool).
		if got := len(calls[n].Prompt.Messages); got != 1+2*n {
			t.Errorf("step %d prompt messages = %d, want %d", n+1, got, 1+2*n)
		}
	}

	// result.toolCalls and result.toolResults
	var callIDs, resultIDs []string
	for _, c := range r.ToolCalls() {
		callIDs = append(callIDs, c.ID)
	}
	for _, tr := range r.ToolResults() {
		resultIDs = append(resultIDs, tr.ToolCallID)
	}
	wantCalls := []string{r81streamCodeExecID}
	var wantResults []string
	for _, roll := range r81streamDiceRolls {
		wantCalls = append(wantCalls, roll[0])
		wantResults = append(wantResults, roll[0])
	}
	wantResults = append(wantResults, r81streamCodeExecID)
	if fmt.Sprint(callIDs) != fmt.Sprint(wantCalls) {
		t.Errorf("toolCalls = %v, want %v", callIDs, wantCalls)
	}
	if fmt.Sprint(resultIDs) != fmt.Sprint(wantResults) {
		t.Errorf("toolResults = %v, want %v", resultIDs, wantResults)
	}
	final := r.FinalStep()
	if len(final.ToolCalls) != 0 {
		t.Errorf("finalStep.toolCalls = %v, want []", final.ToolCalls)
	}
	if fr := final.ToolResults; len(fr) != 1 || fr[0].ToolCallID != r81streamCodeExecID || fr[0].ToolName != "code_execution" ||
		!fr[0].ProviderExecuted || fr[0].Input != nil || fr[0].Dynamic || fr[0].Error != nil ||
		r81streamJSON(t, fr[0].Result) != r81streamJSON(t, map[string]interface{}{"type": "code_execution_result", "stdout": r81streamDiceStdout, "stderr": "", "return_code": 0, "content": []interface{}{}}) {
		t.Errorf("finalStep.toolResults = %+v", fr)
	}

	// tool execution
	if fmt.Sprint(rolls) != "[player1 player2 player1 player2]" {
		t.Errorf("rollDie executions = %v", rolls)
	}

	// result.steps, text, finishReason
	wantReasons := "[tool-calls tool-calls tool-calls tool-calls stop]"
	var reasons []types.FinishReason
	for _, s := range r.Steps() {
		reasons = append(reasons, s.FinishReason)
	}
	if fmt.Sprint(reasons) != wantReasons {
		t.Errorf("step finish reasons = %v", reasons)
	}
	if !strings.Contains(r.Text(), "Game Results") || r.FinishReason() != types.FinishReasonStop {
		t.Errorf("text = %q, finishReason = %q", r.Text(), r.FinishReason())
	}

	// result.totalUsage / onFinish usage
	wantUsage := `{"inputTokens":7920,"inputTokenDetails":{"noCacheTokens":7920},"outputTokens":922,"outputTokenDetails":{"textTokens":922},"totalTokens":8842}`
	if got := r81streamJSON(t, r.Usage()); got != wantUsage {
		t.Errorf("totalUsage = %s, want %s", got, wantUsage)
	}

	// prepareStep calls
	wantPrepare := []prepareCall{{"mock-model-id", 0, 0, 1}, {"mock-model-id", 1, 1, 3}, {"mock-model-id", 2, 2, 5}, {"mock-model-id", 3, 3, 7}, {"mock-model-id", 4, 4, 9}}
	if fmt.Sprint(prepareCalls) != fmt.Sprint(wantPrepare) {
		t.Errorf("prepareStep calls = %+v, want %+v", prepareCalls, wantPrepare)
	}

	// onStepFinish
	reasons = nil
	for _, e := range stepEvents {
		reasons = append(reasons, e.FinishReason)
	}
	if fmt.Sprint(reasons) != wantReasons {
		t.Errorf("onStepFinish finish reasons = %v", reasons)
	}
	for n := 0; n < 2 && n < len(stepEvents); n++ {
		if got := r81streamJSON(t, stepEvents[n].ProviderMetadata); got != wantOpts {
			t.Errorf("onStepFinish[%d].providerMetadata = %s, want %s", n, got, wantOpts)
		}
	}

	// onFinish
	if !strings.Contains(endEvent.Text, "Game Results") || endEvent.FinishReason != types.FinishReasonStop || len(endEvent.Steps) != 5 || len(endEvent.ResponseMessages) != 9 {
		t.Errorf("onFinish text=%q finishReason=%q steps=%d responseMessages=%d", endEvent.Text, endEvent.FinishReason, len(endEvent.Steps), len(endEvent.ResponseMessages))
	}
	if got := r81streamJSON(t, endEvent.Usage); got != wantUsage {
		t.Errorf("onFinish usage = %s, want %s", got, wantUsage)
	}
}
