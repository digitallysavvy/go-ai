package xai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// xaiChanAudioStream is a provider.AudioStream test double backed by a
// channel of chunks; closing the channel signals io.EOF.
type xaiChanAudioStream struct {
	chunks chan []byte

	mu        sync.Mutex
	cancelled bool
}

func newXAIChanAudioStream(chunks ...[]byte) *xaiChanAudioStream {
	ch := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &xaiChanAudioStream{chunks: ch}
}

func newXAIBlockingAudioStream() *xaiChanAudioStream {
	return &xaiChanAudioStream{chunks: make(chan []byte)}
}

// xaiFailingAudioStream is a provider.AudioStream test double whose Next
// always fails with a fixed, non-EOF error, mirroring a rejected
// `audioReader.read()` in TS.
type xaiFailingAudioStream struct {
	err error

	mu        sync.Mutex
	cancelled bool
	cancelErr error
}

func newXAIFailingAudioStream(err error) *xaiFailingAudioStream {
	return &xaiFailingAudioStream{err: err}
}

func (s *xaiFailingAudioStream) Next(ctx context.Context) ([]byte, error) {
	return nil, s.err
}

func (s *xaiFailingAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
	s.cancelErr = reason
}

func (s *xaiFailingAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func (s *xaiChanAudioStream) Next(ctx context.Context) ([]byte, error) {
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

func (s *xaiChanAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

func (s *xaiChanAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// xaiSTTTestServer runs a minimal xAI streaming transcription WebSocket
// endpoint: it records received binary audio chunks and JSON control
// messages (distinguished the same way the production server would: JSON
// decodes, raw audio doesn't), and lets the test script server->client
// events over a channel.
type xaiSTTTestServer struct {
	ts *httptest.Server

	mu             sync.Mutex
	receivedFrames []map[string]interface{}
	receivedAudio  [][]byte
	requestURL     *url.URL
	requestHeaders map[string][]string

	toSend    chan interface{}
	closeConn chan struct{}
}

func newXAISTTTestServer(t *testing.T) *xaiSTTTestServer {
	t.Helper()
	s := &xaiSTTTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
	handlerFn := func(conn *websocket.Conn) {
		s.mu.Lock()
		s.requestURL = conn.Request().URL
		s.requestHeaders = map[string][]string(conn.Request().Header.Clone())
		s.mu.Unlock()

		// Closes the underlying connection on demand (simulating an
		// unexpected close) without shutting down the whole httptest.Server.
		go func() {
			<-s.closeConn
			_ = conn.Close()
		}()

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

		for {
			var raw []byte
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(raw, &decoded); err == nil {
				s.mu.Lock()
				s.receivedFrames = append(s.receivedFrames, decoded)
				s.mu.Unlock()
				continue
			}
			s.mu.Lock()
			s.receivedAudio = append(s.receivedAudio, append([]byte(nil), raw...))
			s.mu.Unlock()
		}
	}
	s.ts = httptest.NewServer(websocket.Server{Handler: handlerFn})
	return s
}

func (s *xaiSTTTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *xaiSTTTestServer) closeConnection() {
	close(s.closeConn)
}

func (s *xaiSTTTestServer) send(v interface{}) {
	s.toSend <- v
}

func (s *xaiSTTTestServer) waitForAudioCount(t *testing.T, n int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.receivedAudio) >= n {
			audio := append([][]byte(nil), s.receivedAudio...)
			s.mu.Unlock()
			return audio
		}
		s.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d audio frame(s)", n)
	return nil
}

func (s *xaiSTTTestServer) waitForFrame(t *testing.T, wantType string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, f := range s.receivedFrames {
			if f["type"] == wantType {
				s.mu.Unlock()
				return f
			}
		}
		s.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a %q frame", wantType)
	return nil
}

func newTestXAITranscriptionModel(baseURL string) *TranscriptionModel {
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	return NewTranscriptionModel(p, "")
}

func drainXAIStream(stream provider.TranscriptionStream) ([]provider.TranscriptionStreamPart, error) {
	var parts []provider.TranscriptionStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, *part)
	}
}

// TS: "should require channels when streaming multichannel audio"
func TestTranscriptionModel_DoStream_RequiresChannelsForMultichannel(t *testing.T) {
	model := newTestXAITranscriptionModel("https://api.x.ai/v1")
	rate := 16000
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"multichannel": true},
		},
	})
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) {
		t.Fatalf("DoStream() error = %v (%T), want *providererrors.InvalidArgumentError", err, err)
	}
	if invalidArg.Field != "providerOptions" {
		t.Errorf("Field = %q, want %q", invalidArg.Field, "providerOptions")
	}
	wantMsg := "providerOptions.xai.channels is required when providerOptions.xai.multichannel is true"
	if invalidArg.Message != wantMsg {
		t.Errorf("Message = %q, want %q", invalidArg.Message, wantMsg)
	}
}

// TS: "should stream xAI STT over WebSocket"
func TestTranscriptionModel_DoStream_HappyPath(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	rate := 16000
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"language": "en",
				"diarize":  true,
				"keyterm":  []interface{}{"AI SDK", "Grok"},
				"streaming": map[string]interface{}{
					"interimResults":   true,
					"endpointing":      500,
					"smartTurn":        0.7,
					"smartTurnTimeout": 3000,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	errCh := make(chan error, 1)
	go func() {
		parts, err := drainXAIStream(result.Stream)
		partsCh <- parts
		errCh <- err
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForAudioCount(t, 1, time.Second)
	server.waitForFrame(t, "audio.done", time.Second)

	server.mu.Lock()
	reqURL := server.requestURL
	headers := server.requestHeaders
	audio := append([][]byte(nil), server.receivedAudio...)
	server.mu.Unlock()

	if reqURL == nil {
		t.Fatal("server never recorded a request URL")
	}
	q := reqURL.Query()
	wantQuery := map[string]string{
		"sample_rate":        "16000",
		"encoding":           "pcm",
		"language":           "en",
		"diarize":            "true",
		"interim_results":    "true",
		"endpointing":        "500",
		"smart_turn":         "0.7",
		"smart_turn_timeout": "3000",
	}
	for k, want := range wantQuery {
		if got := q.Get(k); got != want {
			t.Errorf("query %q = %q, want %q", k, got, want)
		}
	}
	if got := q["keyterm"]; len(got) != 2 || got[0] != "AI SDK" || got[1] != "Grok" {
		t.Errorf("query keyterm = %v, want [AI SDK Grok]", got)
	}
	if authHeaders, ok := headers["Authorization"]; !ok || len(authHeaders) == 0 || authHeaders[0] != "Bearer test-api-key" {
		t.Errorf("Authorization header = %v, want [Bearer test-api-key]", headers["Authorization"])
	}
	if len(audio) != 1 || string(audio[0]) != string([]byte{1, 2, 3}) {
		t.Fatalf("received audio = %v, want one frame with [1 2 3]", audio)
	}

	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "Hel", "is_final": false, "speech_final": false,
		"start": 0, "duration": 0.5,
	})
	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "Hello", "is_final": true, "speech_final": true,
		"start": 0, "duration": 1,
	})
	server.send(map[string]interface{}{"type": "transcript.done", "text": "Hello", "duration": 1})

	parts := <-partsCh
	if err := <-errCh; err != nil {
		t.Fatalf("stream error = %v", err)
	}

	if len(parts) != 4 {
		t.Fatalf("got %d parts, want 4: %+v", len(parts), parts)
	}
	if parts[0].Type != provider.TranscriptionStreamPartTypeStreamStart {
		t.Errorf("parts[0].Type = %q, want stream-start", parts[0].Type)
	}
	if parts[1].Type != provider.TranscriptionStreamPartTypePartial || parts[1].Text != "Hel" {
		t.Errorf("parts[1] = %+v, want a Hel partial", parts[1])
	}
	if parts[2].Type != provider.TranscriptionStreamPartTypeFinal || parts[2].Text != "Hello" {
		t.Errorf("parts[2] = %+v, want a Hello final", parts[2])
	}
	if parts[2].EndSecond == nil || *parts[2].EndSecond != 1 {
		t.Errorf("parts[2].EndSecond = %v, want 1", parts[2].EndSecond)
	}
	finish := parts[3]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish {
		t.Errorf("parts[3].Type = %q, want finish", finish.Type)
	}
	if finish.FinishText != "Hello" || finish.Language != "en" {
		t.Errorf("finish = %+v, want text=Hello language=en", finish)
	}
	if finish.DurationInSeconds == nil || *finish.DurationInSeconds != 1 {
		t.Errorf("finish.DurationInSeconds = %v, want 1", finish.DurationInSeconds)
	}
}

// TS: "should join finalized utterances per channel when transcript.done is
// empty"
func TestTranscriptionModel_DoStream_ReconstructsEmptyFinishText(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)

	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "First utterance.", "is_final": true, "speech_final": true,
		"start": 0, "duration": 1,
	})
	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "Second utterance.", "is_final": true, "speech_final": true,
		"start": 1, "duration": 1,
	})
	server.send(map[string]interface{}{"type": "transcript.done", "text": "", "duration": 2})

	parts := <-partsCh
	last := parts[len(parts)-1]
	if last.Type != provider.TranscriptionStreamPartTypeFinish {
		t.Fatalf("last part type = %q, want finish", last.Type)
	}
	if last.FinishText != "First utterance. Second utterance." {
		t.Errorf("FinishText = %q, want %q", last.FinishText, "First utterance. Second utterance.")
	}
}

// TS: "should error the stream with the server message on error events"
func TestTranscriptionModel_DoStream_ErrorEvent(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	errCh := make(chan error, 1)
	go func() {
		_, err := drainXAIStream(result.Stream)
		errCh <- err
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)
	server.send(map[string]interface{}{"type": "error", "message": "invalid sample_rate"})

	err = <-errCh
	if err == nil || !strings.Contains(err.Error(), "invalid sample_rate") {
		t.Fatalf("stream error = %v, want containing %q", err, "invalid sample_rate")
	}
}

// TS: "should close the WebSocket and stop reading audio when the stream is
// cancelled"
func TestTranscriptionModel_DoStream_CloseCancelsAudio(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	audio := newXAIBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	server.send(map[string]interface{}{"type": "transcript.created"})
	// Give pumpAudio a moment to start (it blocks on audio.Next()).
	time.Sleep(20 * time.Millisecond)

	if err := result.Stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !audio.wasCancelled() {
		time.Sleep(2 * time.Millisecond)
	}
	if !audio.wasCancelled() {
		t.Error("audio stream was not cancelled after Close()")
	}
}

// TS: "should warn on unrecognized inputAudioFormat types"
func TestTranscriptionModel_DoStream_WarnsOnUnrecognizedAudioFormat(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/wav"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)
	server.send(map[string]interface{}{"type": "transcript.done", "text": "", "duration": 0})

	parts := <-partsCh
	if len(parts) == 0 || parts[0].Type != provider.TranscriptionStreamPartTypeStreamStart {
		t.Fatalf("parts[0] = %+v, want stream-start", parts)
	}
	if len(parts[0].Warnings) != 1 || parts[0].Warnings[0].Type != "other" {
		t.Fatalf("warnings = %+v, want one 'other' warning", parts[0].Warnings)
	}

	server.mu.Lock()
	reqURL := server.requestURL
	server.mu.Unlock()
	if got := reqURL.Query().Get("encoding"); got != "pcm" {
		t.Errorf("encoding = %q, want pcm (fallback)", got)
	}
}

// A non-EOF audio read failure must fail the stream and cancel the audio
// source, mirroring TS's `sendAudio(socket).catch(finishWithError)` (a
// rejected `audioReader.read()` fails the stream exactly like a failed
// `socket.send`). Regression test for a bug where pumpAudio silently
// returned on a non-EOF read error instead of reporting it.
func TestTranscriptionModel_DoStream_AudioReadFailureFailsStream(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	readErr := errors.New("boom: audio source failed")
	audio := newXAIFailingAudioStream(readErr)
	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	errCh := make(chan error, 1)
	go func() {
		_, err := drainXAIStream(result.Stream)
		errCh <- err
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), readErr.Error()) {
			t.Fatalf("stream error = %v, want containing %q", err, readErr.Error())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stream to fail after a read error")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !audio.wasCancelled() {
		time.Sleep(2 * time.Millisecond)
	}
	if !audio.wasCancelled() {
		t.Error("audio stream was not cancelled after a read failure")
	}
}

// TS: "should cancel the audio stream when the WebSocket constructor throws"
// (adapted: Go has no pluggable WebSocket constructor, so a dial failure is
// exercised instead by pointing at a host that refuses the connection).
func TestTranscriptionModel_DoStream_DialFailureCancelsAudio(t *testing.T) {
	// A closed listener's address is guaranteed to refuse the connection.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	model := newTestXAITranscriptionModel("http://" + addr)
	audio := newXAIChanAudioStream([]byte{1, 2, 3})
	result, doStreamErr := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if doStreamErr != nil {
		t.Fatalf("DoStream() error = %v", doStreamErr)
	}
	defer result.Stream.Close() //nolint:errcheck

	_, err = drainXAIStream(result.Stream)
	if err == nil {
		t.Fatal("expected an error from a refused dial")
	}
	if !audio.wasCancelled() {
		t.Error("audio stream was not cancelled after a dial failure")
	}
}

// TS: "should treat is_final fragments as partials and use the speech_final
// text for finish"
func TestTranscriptionModel_DoStream_IsFinalFragmentsStayPartial(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)

	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "No", "is_final": true, "speech_final": false,
		"start": 0, "duration": 0.5,
	})
	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": ", I'm not", "is_final": true, "speech_final": false,
		"start": 0.5, "duration": 0.7,
	})
	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "No, I'm not.", "is_final": true, "speech_final": true,
		"start": 0, "duration": 1.2,
	})
	server.send(map[string]interface{}{"type": "transcript.done", "text": "", "duration": 1.2})

	parts := <-partsCh
	var gotTypes []string
	for _, p := range parts {
		gotTypes = append(gotTypes, p.Type)
	}
	wantTypes := []string{
		provider.TranscriptionStreamPartTypeStreamStart,
		provider.TranscriptionStreamPartTypePartial,
		provider.TranscriptionStreamPartTypePartial,
		provider.TranscriptionStreamPartTypeFinal,
		provider.TranscriptionStreamPartTypeFinish,
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("part types = %v, want %v", gotTypes, wantTypes)
	}
	for i, want := range wantTypes {
		if gotTypes[i] != want {
			t.Errorf("parts[%d].Type = %q, want %q", i, gotTypes[i], want)
		}
	}
	last := parts[len(parts)-1]
	if last.FinishText != "No, I'm not." {
		t.Errorf("FinishText = %q, want %q", last.FinishText, "No, I'm not.")
	}
}

// TS: "should fall back to the latest pending text when no speech_final
// arrived before transcript.done"
func TestTranscriptionModel_DoStream_FallsBackToPendingTextOnDone(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()

	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()

	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)

	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "Hello wor", "is_final": false, "speech_final": false,
	})
	server.send(map[string]interface{}{
		"type": "transcript.partial", "text": "Hello world", "is_final": true, "speech_final": false,
	})
	server.send(map[string]interface{}{"type": "transcript.done", "text": "", "duration": 1})

	parts := <-partsCh
	last := parts[len(parts)-1]
	if last.Type != provider.TranscriptionStreamPartTypeFinish || last.FinishText != "Hello world" {
		t.Errorf("last part = %+v, want finish with text %q", last, "Hello world")
	}
}

func intPtr(v int) *int { return &v }
