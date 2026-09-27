package cartesia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// chanAudioStream is a provider.AudioStream test double backed by a channel
// of chunks; closing the channel signals io.EOF.
type chanAudioStream struct {
	chunks chan []byte

	mu          sync.Mutex
	cancelled   bool
	cancelCount int
}

func newChanAudioStream(chunks ...[]byte) *chanAudioStream {
	ch := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &chanAudioStream{chunks: ch}
}

// newBlockingAudioStream returns an AudioStream whose Next never yields a
// chunk or EOF until its context is done, for tests that need audio to still
// be "streaming" indefinitely.
func newBlockingAudioStream() *chanAudioStream {
	return &chanAudioStream{chunks: make(chan []byte)}
}

func (s *chanAudioStream) Next(ctx context.Context) ([]byte, error) {
	select {
	case c, ok := <-s.chunks:
		if !ok {
			return nil, io.EOF
		}
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *chanAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
	s.cancelCount++
}

func (s *chanAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// cartesiaRealtimeTestServer runs a minimal Cartesia Ink 2 realtime
// transcription endpoint: an HTTP POST /access-token handler plus a
// WebSocket handler (mounted at both /stt/websocket and
// /stt/turns/websocket) that records every received client frame (as raw
// bytes, since golang.org/x/net/websocket's Message codec ignores the wire
// frame type and decodes purely by the target Go type) and lets the test
// script server->client JSON events over a channel.
type cartesiaRealtimeTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received [][]byte

	toSend    chan interface{}
	closeConn chan struct{}
}

func newCartesiaRealtimeTestServer(t *testing.T) *cartesiaRealtimeTestServer {
	t.Helper()
	s := &cartesiaRealtimeTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
	wsHandlerFn := func(conn *websocket.Conn) {
		go func() {
			for msg := range s.toSend {
				encoded, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				if err := websocket.Message.Send(conn, string(encoded)); err != nil {
					return
				}
			}
		}()
		go func() {
			<-s.closeConn
			_ = conn.Close()
		}()
		for {
			var raw []byte
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			cp := make([]byte, len(raw))
			copy(cp, raw)
			s.mu.Lock()
			s.received = append(s.received, cp)
			s.mu.Unlock()
		}
	}
	wsHandler := websocket.Server{Handler: wsHandlerFn}
	mux := http.NewServeMux()
	mux.HandleFunc("/access-token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-access-token"})
	})
	mux.Handle("/stt/turns/websocket", wsHandler)
	mux.Handle("/stt/websocket", wsHandler)
	s.ts = httptest.NewServer(mux)
	return s
}

func (s *cartesiaRealtimeTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *cartesiaRealtimeTestServer) all() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.received))
	copy(out, s.received)
	return out
}

func (s *cartesiaRealtimeTestServer) waitForCount(t *testing.T, n int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := s.all(); len(got) >= n {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d received messages; got %v", n, s.all())
	return nil
}

func newCartesiaRealtimeTestModel(baseURL string) *TranscriptionModel {
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	model, _ := p.TranscriptionModel(ModelInk2)
	return model.(*TranscriptionModel)
}

func drainUntilFinishOrError(t *testing.T, stream provider.TranscriptionStream) ([]*provider.TranscriptionStreamPart, error) {
	t.Helper()
	var parts []*provider.TranscriptionStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, part)
		if part.Type == provider.TranscriptionStreamPartTypeFinish {
			return parts, nil
		}
	}
}

func boolPtr(v bool) *bool { return &v }
func rate(v int) *int      { return &v }

// TestTranscriptionModel_DoStream_RejectsNonStreamingModel mirrors part of TS
// "rejects unsupported model operations and Ink 2 languages" (the doStream
// call with ink-whisper).
func TestTranscriptionModel_DoStream_RejectsNonStreamingModel(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model, _ := p.TranscriptionModel(ModelInkWhisper)
	streamer := model.(provider.TranscriptionStreamer)

	_, err := streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if !errorsIsUnsupported(err) {
		t.Fatalf("err = %v, want ErrUnsupportedFeature", err)
	}
}

func errorsIsUnsupported(err error) bool {
	return err != nil && (err == providererrors.ErrUnsupportedFeature || strings.Contains(err.Error(), "unsupported"))
}

// TestTranscriptionModel_DoStream_RejectsNonEnglishLanguage mirrors part of
// TS "rejects unsupported model operations and Ink 2 languages" (the "es"
// providerOptions.language rejection).
func TestTranscriptionModel_DoStream_RejectsNonEnglishLanguage(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions:  map[string]interface{}{"cartesia": TranscriptionModelOptions{Language: "es"}},
	})
	if err == nil || !strings.Contains(err.Error(), "currently supports English only") {
		t.Fatalf("err = %v", err)
	}
}

// TestTranscriptionModel_DoStream_RejectsUnsupportedInputFormat mirrors TS
// "still validates inputAudioFormat.type when streaming.encoding is set":
// the explicit encoding override must not bypass the media-type allowlist.
func TestTranscriptionModel_DoStream_RejectsUnsupportedInputFormat(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/mpeg", Rate: rate(44100)},
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Streaming: &StreamingOptions{Encoding: "pcm_s16le"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Unsupported Cartesia streaming audio format: audio/mpeg") {
		t.Fatalf("err = %v", err)
	}
}

// TestTranscriptionModel_DoStream_TurnDetected mirrors TS "streams Ink 2
// turn-detected transcription over WebSocket".
func TestTranscriptionModel_DoStream_TurnDetected(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions:  map[string]interface{}{"cartesia": TranscriptionModelOptions{Language: "en"}},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	wsURL := result.RequestBody.(string)
	for _, want := range []string{"/stt/turns/websocket", "model=ink-2", "encoding=pcm_s16le", "sample_rate=16000", "cartesia_version="} {
		if !strings.Contains(wsURL, want) {
			t.Fatalf("request url = %q, want to contain %q", wsURL, want)
		}
	}
	if strings.Contains(wsURL, "access_token") {
		t.Fatalf("request url = %q, want access_token stripped", wsURL)
	}

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	if first.Type != provider.TranscriptionStreamPartTypeStreamStart || len(first.Warnings) != 0 {
		t.Fatalf("first = %+v", first)
	}

	// One audio chunk plus the turn-detection close control message.
	msgs := server.waitForCount(t, 2, time.Second)
	if string(msgs[0]) != "\x01\x02\x03" {
		t.Fatalf("msgs[0] = %q, want raw audio bytes", msgs[0])
	}
	var closeMsg map[string]string
	if jsonErr := json.Unmarshal(msgs[1], &closeMsg); jsonErr != nil || closeMsg["type"] != "close" {
		t.Fatalf("msgs[1] = %q, want {\"type\":\"close\"}", msgs[1])
	}

	server.toSend <- map[string]interface{}{"type": "turn.update", "request_id": "turn-1", "transcript": "Hello"}
	server.toSend <- map[string]interface{}{"type": "turn.end", "request_id": "turn-1", "transcript": "Hello world"}
	server.toSend <- map[string]interface{}{"type": "turn.end", "request_id": "turn-1", "transcript": "How are you?"}
	server.toSend <- map[string]interface{}{"type": "done"}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 4 {
		t.Fatalf("len(rest) = %d, want 4; got %+v", len(rest), rest)
	}
	if rest[0].Type != provider.TranscriptionStreamPartTypePartial || rest[0].ID != "turn-1" || rest[0].Text != "Hello" {
		t.Fatalf("rest[0] = %+v", rest[0])
	}
	if rest[1].Type != provider.TranscriptionStreamPartTypeFinal || rest[1].Text != "Hello world" {
		t.Fatalf("rest[1] = %+v", rest[1])
	}
	if rest[2].Type != provider.TranscriptionStreamPartTypeFinal || rest[2].Text != "How are you?" {
		t.Fatalf("rest[2] = %+v", rest[2])
	}
	finish := rest[3]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello world How are you?" || finish.Language != "en" {
		t.Fatalf("finish = %+v", finish)
	}
	if len(finish.Segments) != 0 {
		t.Fatalf("segments = %+v, want empty", finish.Segments)
	}
}

// TestTranscriptionModel_DoStream_ManualFinalization mirrors TS "supports
// manual finalization when turn detection is disabled".
func TestTranscriptionModel_DoStream_ManualFinalization(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcmu", Rate: rate(8000)},
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Language: "en", Streaming: &StreamingOptions{TurnDetection: boolPtr(false)}},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	wsURL := result.RequestBody.(string)
	if !strings.Contains(wsURL, "/stt/websocket") || !strings.Contains(wsURL, "encoding=pcm_mulaw") || !strings.Contains(wsURL, "language=en") {
		t.Fatalf("request url = %q", wsURL)
	}

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	// One audio chunk plus the "finalize" control message.
	msgs := server.waitForCount(t, 2, time.Second)
	if string(msgs[1]) != "finalize" {
		t.Fatalf("msgs[1] = %q, want \"finalize\"", msgs[1])
	}

	// Queued upfront: draining the stream (below) is what actually lets
	// run()'s single select loop advance past each blocking emit() to reach
	// the flush_done/done handling, so waiting on the server's received
	// messages here (before any drain) would deadlock.
	server.toSend <- map[string]interface{}{"type": "transcript", "request_id": "request-1", "text": "Hello ", "is_final": true, "duration": 0.5}
	server.toSend <- map[string]interface{}{"type": "transcript", "request_id": "request-1", "text": "world", "is_final": true, "duration": 0.4}
	server.toSend <- map[string]interface{}{"type": "flush_done", "request_id": "request-1"}
	server.toSend <- map[string]interface{}{"type": "done", "request_id": "request-1"}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}

	// The flush_done handler's "close" reply must have reached the server by
	// the time the stream finishes.
	closeMsgs := server.waitForCount(t, 3, time.Second)
	if string(closeMsgs[2]) != "close" {
		t.Fatalf("msgs[2] = %q, want \"close\"", closeMsgs[2])
	}

	finish := rest[len(rest)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello world" {
		t.Fatalf("finish = %+v", finish)
	}
	if finish.DurationInSeconds == nil || *finish.DurationInSeconds != 0.9 {
		t.Fatalf("durationInSeconds = %v", finish.DurationInSeconds)
	}
}

// TestTranscriptionModel_DoStream_LinearPCMWideningNoWarning mirrors TS
// "supports the %s streaming PCM encoding without a warning".
func TestTranscriptionModel_DoStream_LinearPCMWideningNoWarning(t *testing.T) {
	for _, encoding := range []string{"pcm_f16le", "pcm_f32le", "pcm_s32le"} {
		t.Run(encoding, func(t *testing.T) {
			server := newCartesiaRealtimeTestServer(t)
			defer server.close()

			model := newCartesiaRealtimeTestModel(server.ts.URL)
			result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio:            newChanAudioStream(),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(48000)},
				ProviderOptions: map[string]interface{}{
					"cartesia": TranscriptionModelOptions{Streaming: &StreamingOptions{Encoding: encoding}},
				},
			})
			if err != nil {
				t.Fatalf("DoStream() error = %v", err)
			}
			defer result.Stream.Close() //nolint:errcheck

			wsURL := result.RequestBody.(string)
			if !strings.Contains(wsURL, "encoding="+encoding) {
				t.Fatalf("request url = %q, want encoding=%s", wsURL, encoding)
			}

			first, err := result.Stream.Next()
			if err != nil {
				t.Fatalf("Stream.Next() error = %v", err)
			}
			if first.Type != provider.TranscriptionStreamPartTypeStreamStart || len(first.Warnings) != 0 {
				t.Fatalf("first = %+v, want stream-start with no warnings", first)
			}
		})
	}
}

// TestTranscriptionModel_DoStream_WarnsOnContradictingEncoding mirrors TS
// "warns when streaming.encoding contradicts the declared audio format".
func TestTranscriptionModel_DoStream_WarnsOnContradictingEncoding(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcmu", Rate: rate(8000)},
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Streaming: &StreamingOptions{Encoding: "pcm_f32le"}},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	wsURL := result.RequestBody.(string)
	if !strings.Contains(wsURL, "encoding=pcm_f32le") {
		t.Fatalf("request url = %q", wsURL)
	}

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	if len(first.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want 1", first.Warnings)
	}
	want := "providerOptions.cartesia.streaming.encoding 'pcm_f32le' contradicts inputAudioFormat.type 'audio/pcmu' (inferred 'pcm_mulaw'); sending 'pcm_f32le'."
	if first.Warnings[0].Message != want {
		t.Fatalf("warning = %q, want %q", first.Warnings[0].Message, want)
	}
}

// TestTranscriptionModel_DoStream_WarnsOnG711Override mirrors TS "warns when
// generic audio/pcm is overridden to a G.711 encoding".
func TestTranscriptionModel_DoStream_WarnsOnG711Override(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(8000)},
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Streaming: &StreamingOptions{Encoding: "pcm_mulaw"}},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	want := "providerOptions.cartesia.streaming.encoding 'pcm_mulaw' contradicts inputAudioFormat.type 'audio/pcm' (inferred 'pcm_s16le'); sending 'pcm_mulaw'."
	if len(first.Warnings) != 1 || first.Warnings[0].Message != want {
		t.Fatalf("warnings = %+v, want [%q]", first.Warnings, want)
	}
}

// TestTranscriptionModel_DoStream_TimestampGranularitiesWarning mirrors the
// providerOptions.cartesia.timestampGranularities unsupported-for-streaming
// warning.
func TestTranscriptionModel_DoStream_TimestampGranularitiesWarning(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{TimestampGranularities: []string{"word"}},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	if len(first.Warnings) != 1 || first.Warnings[0].Feature != "providerOptions.cartesia.timestampGranularities" {
		t.Fatalf("warnings = %+v", first.Warnings)
	}
}

// TestTranscriptionModel_DoStream_CancelsAudioWhenAccessTokenFails verifies
// that a failure obtaining the streaming access token (before the WebSocket
// stream, and thus the caller's AudioStream, is ever created) surfaces as an
// error without touching the AudioStream.
func TestTranscriptionModel_DoStream_CancelsAudioWhenAccessTokenFails(t *testing.T) {
	audio := newBlockingAudioStream()
	// An httptest server that is immediately closed guarantees a refused
	// connection without relying on a specific unused port being free.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	model := newCartesiaRealtimeTestModel(ts.URL)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err == nil {
		t.Fatal("expected an error obtaining the access token")
	}
	if audio.wasCancelled() {
		t.Fatal("AudioStream must not be touched before the WebSocket stream is created")
	}
}

// TestTranscriptionModel_DoStream_CancelsAudioWhenDialFails mirrors TS
// "cancels input when the WebSocket constructor fails": once the access
// token is obtained, a WebSocket dial failure must cancel the caller's
// AudioStream and surface the dial error on the stream.
func TestTranscriptionModel_DoStream_CancelsAudioWhenDialFails(t *testing.T) {
	audio := newBlockingAudioStream()
	// /access-token succeeds, but nothing is mounted at the streaming
	// WebSocket path, so the upgrade handshake fails.
	mux := http.NewServeMux()
	mux.HandleFunc("/access-token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-access-token"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	model := newCartesiaRealtimeTestModel(ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil {
		t.Fatal("expected a dial error")
	}
	if !audio.wasCancelled() {
		t.Fatal("expected the AudioStream to be cancelled when the dial fails")
	}
}

// TestTranscriptionModel_DoStream_CancelMidStream exercises Close() while the
// stream is actively receiving, verifying Next() unblocks and the caller's
// AudioStream is cancelled (run with -race).
func TestTranscriptionModel_DoStream_CancelMidStream(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	audio := newBlockingAudioStream()
	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := result.Stream.Close(); err != nil {
			t.Errorf("Stream.Close() error = %v", err)
		}
	}()
	<-done

	if _, err := result.Stream.Next(); err == nil {
		t.Fatal("expected Stream.Next() to return an error after Close()")
	}
	if !audio.wasCancelled() {
		t.Fatal("expected the AudioStream to be cancelled when the stream is closed")
	}
}

// TestTranscriptionModel_DoStream_ErrorMessage verifies the "error" event
// type surfaces its message on the stream.
func TestTranscriptionModel_DoStream_ErrorMessage(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()

	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newBlockingAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	server.toSend <- map[string]interface{}{"type": "error", "message": "boom"}

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want an error containing 'boom'", err)
	}
}
