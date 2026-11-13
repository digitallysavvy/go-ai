package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

// TS gatewayAvailableModelsResponseSchema: pricing is nullish, but a pricing
// object requires input and output strings, so a model without them makes
// the whole response invalid (createJsonResponseHandler "Invalid JSON
// response", a GatewayResponseError).
func TestR95provgMetadataPricingRequiresInputAndOutput(t *testing.T) {
	model := func(pricing string) string {
		return `{"models":[{"id":"a/b","name":"B","pricing":` + pricing +
			`,"specification":{"specificationVersion":"v4","provider":"a","modelId":"b"}}]}`
	}
	for _, tc := range []struct {
		pricing string
		valid   bool
	}{
		{`null`, true},
		{`{"input":"1","output":"2"}`, true},
		{`{"input":"1","output":"2","input_cache_read":null}`, true},
		{`{"output":"2"}`, false},
		{`{"input":"1"}`, false},
		{`{}`, false},
	} {
		err := validateMetadataResponse([]byte(model(tc.pricing)))
		if (err == nil) != tc.valid {
			t.Errorf("pricing %s: err = %v, want valid=%v", tc.pricing, err, tc.valid)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(model(`{"input":"1"}`)))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetAvailableModels(context.Background())
	var re *gatewayerrors.GatewayResponseError
	if !errors.As(err, &re) {
		t.Fatalf("err = %T %v, want a GatewayResponseError", err, err)
	}
}

// TS doGenerate response.headers is Object.fromEntries([...response.headers]):
// a repeated response header joins its values with ", ", as DoStart and
// DoStatus report it, not just its first value.
func TestR95provgVideoGenerateRepeatedResponseHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Add("X-R95provg", "a")
		w.Header().Add("X-R95provg", "b")
		_, _ = w.Write([]byte("data: {\"type\":\"result\",\"videos\":[{\"type\":\"url\",\"url\":\"https://x/v.mp4\",\"mediaType\":\"video/mp4\"}]}\n\n"))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewVideoModel(p, "google/veo-3").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p", N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Response.Headers["X-R95provg"]; got != "a, b" {
		t.Fatalf("X-R95provg = %q, want %q (headers %v)", got, "a, b", res.Response.Headers)
	}
}

// TS onSocketError then asGatewayError: a Gateway socket that fails to open
// (a 401 handshake) is a GatewayError with the stream's connection error
// text; a signal aborted before the open is the reason itself.
func TestR95provgStreamRefusedHandshakeAndPreAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res, err := newTestGatewayTranscriptionModel(t, srv.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: &r95provgOpenAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r95provgDrain(t, func() error { _, err := res.Stream.Next(); return err })
	if !gatewayerrors.IsGatewayError(got) || got.Error() != "Invalid error response format: Gateway request failed: Connection error on AI Gateway transcription stream" {
		t.Errorf("transcription: err = %T %v", got, got)
	}
	sres, err := newTestGatewaySpeechModel(t, srv.URL).DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: newSpeechTestText()})
	if err != nil {
		t.Fatal(err)
	}
	got = r95provgDrain(t, func() error { _, err := sres.Stream.Next(); return err })
	if !gatewayerrors.IsGatewayError(got) || got.Error() != "Invalid error response format: Gateway request failed: Connection error on AI Gateway speech stream" {
		t.Errorf("speech: err = %T %v", got, got)
	}

	reason := errors.New("r95provg aborted before open")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	res, err = newTestGatewayTranscriptionModel(t, srv.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: &r95provgOpenAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err == nil {
		if got := r95provgDrain(t, func() error { _, err := res.Stream.Next(); return err }); got != reason {
			t.Errorf("pre-aborted transcription: err = %v, want the reason", got)
		}
	} else if err != reason {
		t.Errorf("pre-aborted transcription: DoStream err = %v", err)
	}
}
