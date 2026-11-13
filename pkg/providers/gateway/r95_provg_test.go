package gateway

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// r95provgOpenAudio is an audio input that never ends on its own and records
// whether the provider cancelled it.
type r95provgOpenAudio struct {
	mu        sync.Mutex
	cancelled bool
}

func (a *r95provgOpenAudio) Next(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (a *r95provgOpenAudio) Cancel(error) {
	a.mu.Lock()
	a.cancelled = true
	a.mu.Unlock()
}

func (a *r95provgOpenAudio) wasCancelled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelled
}

func r95provgDrain(t *testing.T, next func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			if err := next(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end")
		return nil
	}
}

// TS connectToWebSocket onAbort: controller.error(abortSignal.reason). The
// abort reason reaches the caller as is, not through asGatewayError (which
// would make a custom reason a GatewayResponseError "Invalid error response
// format: Gateway request failed: ...").
func TestR95provgStreamAbortReasonIsNotWrapped(t *testing.T) {
	reason := errors.New("r95provg user stop")

	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newTestGatewayTranscriptionModel(t, server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: &r95provgOpenAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, transcriptionStreamStartFrameType, 2*time.Second)
	cancel(reason)
	got := r95provgDrain(t, func() error { _, err := res.Stream.Next(); return err })
	if got != reason || gatewayerrors.IsGatewayError(got) {
		t.Errorf("transcription: err = %T %v, want the abort reason itself", got, got)
	}

	server2 := newGatewayTranscriptionTestServer(t)
	defer server2.close()
	text := newSpeechTestText()
	text.block = true
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	sres, err := newTestGatewaySpeechModel(t, server2.ts.URL).DoStreamSpeech(ctx2, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	defer sres.Stream.Close() //nolint:errcheck
	server2.waitForFrame(t, "speech-stream.start", 2*time.Second)
	cancel2(reason)
	got = r95provgDrain(t, func() error { _, err := sres.Stream.Next(); return err })
	if got != reason || gatewayerrors.IsGatewayError(got) {
		t.Errorf("speech: err = %T %v, want the abort reason itself", got, got)
	}
}

// TS cleanup(1000) after the finish part cancels the caller's audio stream
// when it is still open (the server finished before the audio ended), as the
// Google and Vertex Live transcription streams and the Gateway speech stream
// (text input) do.
func TestR95provgTranscriptionFinishCancelsOpenAudio(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	audio := &r95provgOpenAudio{}
	res, err := newTestGatewayTranscriptionModel(t, server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, transcriptionStreamStartFrameType, 2*time.Second)
	server.toSend <- map[string]interface{}{"type": "finish", "text": "hi", "segments": []interface{}{}}
	var last *provider.TranscriptionStreamPart
	for {
		part, err := res.Stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		last = part
	}
	if last == nil || last.Type != provider.TranscriptionStreamPartTypeFinish {
		t.Fatalf("last part = %+v, want finish", last)
	}
	if !audio.wasCancelled() {
		t.Error("the open audio input was not cancelled after the finish part")
	}
}

// Stream parts are provider responses: keys match exactly, as zod (and the
// TS stream handler, which reads part.delta) does. "Delta" is not delta.
func TestR95provgStreamChunkKeysMatchExactly(t *testing.T) {
	var chunk gatewayStreamChunk
	if err := decodeGatewayStreamChunk([]byte(`{"type":"text-delta","id":"t1","Delta":"wrong case"}`), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Delta != "" || chunk.Type != "text-delta" || chunk.ID != "t1" {
		t.Fatalf("chunk = type %q id %q delta %q, want delta empty", chunk.Type, chunk.ID, chunk.Delta)
	}
	// The key-by-key fallback (a field of an unexpected type) matches
	// exactly too.
	chunk = gatewayStreamChunk{}
	if err := decodeGatewayStreamChunk([]byte(`{"type":"text-delta","Delta":"wrong case","usage":"bad"}`), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Delta != "" || chunk.Type != "text-delta" {
		t.Fatalf("fallback chunk = type %q delta %q, want delta empty", chunk.Type, chunk.Delta)
	}
	if bad := jsonexact.UntaggedFields(&gatewayStreamChunk{}); len(bad) > 0 {
		t.Fatalf("stream chunk fields without a json tag: %v", bad)
	}
}
