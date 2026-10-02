package openai

import (
	"context"
	"crypto/sha1" //nolint:gosec // required by the WebSocket handshake spec, not for security
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// chanTestAudioStream is a provider.AudioStream test double backed by a
// channel of chunks, closed to signal EOF.
type chanTestAudioStream struct {
	chunks    chan []byte
	mu        sync.Mutex
	cancelled bool
}

func newChanTestAudioStream(chunks ...[]byte) *chanTestAudioStream {
	ch := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &chanTestAudioStream{chunks: ch}
}

func (s *chanTestAudioStream) Next(ctx context.Context) ([]byte, error) {
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

func (s *chanTestAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

// realtimeTranscriptionTestServer runs a minimal OpenAI realtime transcription
// WebSocket endpoint: it records every received message and lets the test
// script server->client events over a channel.
type realtimeTranscriptionTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}

	toSend    chan interface{}
	closeConn chan struct{}
}

func newRealtimeTranscriptionTestServer(t *testing.T) *realtimeTranscriptionTestServer {
	t.Helper()
	s := &realtimeTranscriptionTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
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
		// Closes the underlying connection on demand (simulating the server
		// closing before any completed/error event), returning from the
		// handler without blocking the still-open httptest.Server.
		go func() {
			<-s.closeConn
			_ = conn.Close()
		}()
		for {
			var raw string
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				continue
			}
			s.mu.Lock()
			s.received = append(s.received, decoded)
			s.mu.Unlock()
		}
	}
	handler := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *stdhttp.Request) error {
			// The real-time subprotocol negotiation offers 1-2 protocols
			// (["realtime"] or ["realtime", "openai-insecure-api-key.<token>"]);
			// AcceptHandshake requires exactly one to be echoed back.
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: wsHandlerFn,
	}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *realtimeTranscriptionTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

// closeConnection closes just the active WebSocket connection, simulating
// the server closing before a completed/error event was ever sent.
func (s *realtimeTranscriptionTestServer) closeConnection() {
	close(s.closeConn)
}

func (s *realtimeTranscriptionTestServer) receivedTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	types := make([]string, 0, len(s.received))
	for _, m := range s.received {
		if t, ok := m["type"].(string); ok {
			types = append(types, t)
		}
	}
	return types
}

func (s *realtimeTranscriptionTestServer) waitForReceived(t *testing.T, want string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, m := range s.received {
			if m["type"] == want {
				s.mu.Unlock()
				return m
			}
		}
		s.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a %q message; got %v", want, s.receivedTypes())
	return nil
}

func newTestTranscriptionModel(baseURL string) *TranscriptionModel {
	p := New(Config{APIKey: "test-key", BaseURL: baseURL})
	return NewTranscriptionModel(p, "gpt-realtime-whisper")
}

// TestTranscriptionModel_BaseWSHeadersCarryUserAgentTag mirrors TS
// openai-transcription-model.ts's doStream, which reuses
// `this.config.headers()` -- the same tagged getHeaders() closure used for
// REST calls -- for the WebSocket handshake, so it carries the
// `ai-sdk-openai/VERSION` tag too.
func TestTranscriptionModel_BaseWSHeadersCarryUserAgentTag(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewTranscriptionModel(p, "gpt-realtime-whisper")

	ua := m.baseWSHeaders()["user-agent"]
	if !strings.HasPrefix(ua, "ai-sdk-openai/") {
		t.Fatalf("user-agent = %q, want ai-sdk-openai/... prefix", ua)
	}
}

func TestTranscriptionModel_DoStream_RejectsNonRealtimeModel(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewTranscriptionModel(p, "whisper-1")
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestTranscriptionModel_DoTranscribe_RejectsRealtimeModel(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewTranscriptionModel(p, "gpt-realtime-whisper")
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1, 2, 3}, MimeType: "audio/wav"})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

// TestTranscriptionModel_DoStream_StreamsDeltasAndFinal mirrors TS
// "should stream transcript parts and resolve final metadata" end-to-end
// through a real WebSocket round trip.
func TestTranscriptionModel_DoStream_StreamsDeltasAndFinal(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestTranscriptionModel(server.ts.URL)

	rate := 16000
	audio := newChanTestAudioStream([]byte{1, 2, 3})
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	appendMsg := server.waitForReceived(t, "input_audio_buffer.append", time.Second)
	if audioB64, _ := appendMsg["audio"].(string); audioB64 == "" {
		t.Fatal("expected base64 audio in input_audio_buffer.append")
	} else if decoded, err := base64.StdEncoding.DecodeString(audioB64); err != nil || string(decoded) != "\x01\x02\x03" {
		t.Fatalf("decoded audio = %q, err = %v", decoded, err)
	}
	server.waitForReceived(t, "input_audio_buffer.commit", time.Second)

	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "Hel"}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "lo"}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item-1", "transcript": "Hello"}

	var parts []*provider.TranscriptionStreamPart
	for {
		part, err := result.Stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Stream.Next() error = %v", err)
		}
		parts = append(parts, part)
		if part.Type == provider.TranscriptionStreamPartTypeFinish {
			break
		}
	}

	if len(parts) != 5 {
		t.Fatalf("len(parts) = %d, want 5 (stream-start, delta, delta, final, finish); got %+v", len(parts), parts)
	}
	if parts[0].Type != provider.TranscriptionStreamPartTypeStreamStart {
		t.Fatalf("parts[0].Type = %s, want stream-start", parts[0].Type)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_EmitsErrorOnRealtimeError verifies an
// `error` server event surfaces as a Stream.Next() error.
func TestTranscriptionModel_DoStream_EmitsErrorOnRealtimeError(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestTranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "boom"}}

	// drain the stream-start part first
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	_, err = result.Stream.Next()
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Stream.Next() error = %v, want containing 'boom'", err)
	}
}

// TestTranscriptionModel_DoStream_PrematureCloseIsNotAnError mirrors TS
// openai-transcription-model.ts's onClose: `if (finished) return; ...
// controller.close()` — a clean WebSocket close before any
// completed/error event ever arrives ends the stream successfully (io.EOF),
// not as a failure, matching a consumer that just sees the stream end.
func TestTranscriptionModel_DoStream_PrematureCloseIsNotAnError(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	audio := newChanTestAudioStream()
	model := newTestTranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)

	// drain the stream-start part
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	server.closeConnection()

	if _, err := result.Stream.Next(); err != io.EOF {
		t.Fatalf("Stream.Next() error = %v, want io.EOF for a premature clean close", err)
	}
}

// failingTestAudioStream is a provider.AudioStream test double whose Next
// returns a fixed non-EOF error, mirroring a rejected `audioReader.read()` in
// TS.
type failingTestAudioStream struct {
	err error

	mu        sync.Mutex
	cancelled bool
}

func (s *failingTestAudioStream) Next(ctx context.Context) ([]byte, error) {
	return nil, s.err
}

func (s *failingTestAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

func (s *failingTestAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// TestTranscriptionModel_DoStream_AudioReadErrorSurfacesError mirrors TS
// `void sendAudio(socket).catch(finishWithError)`: a genuine AudioStream read
// failure (distinct from its natural io.EOF completion) must terminate the
// stream with an error, not be silently swallowed. Regression test for a bug
// where pumpAudio returned on a non-EOF read (or write) error without
// reporting it.
func TestTranscriptionModel_DoStream_AudioReadErrorSurfacesError(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	audio := &failingTestAudioStream{err: errors.New("audio source failed")}
	model := newTestTranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	// drain the stream-start part first
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	_, err = result.Stream.Next()
	if err == nil || !strings.Contains(err.Error(), "audio source failed") {
		t.Fatalf("Stream.Next() error = %v, want containing 'audio source failed'", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !audio.wasCancelled() {
		time.Sleep(2 * time.Millisecond)
	}
	if !audio.wasCancelled() {
		t.Error("audio stream was not cancelled after a read failure")
	}
}

// TestTranscriptionModel_DoStream_AbnormalDisconnectSurfacesError mirrors TS
// connectToWebSocket's onSocketError semantics: an abnormal disconnection
// (here, a connection dropped mid-frame via a TCP RST, distinct from a clean
// WebSocket close frame) must surface as an error rather than a silent
// finish, matching TestTranscriptionModel_DoStream_PrematureCloseIsNotAnError's
// clean-close counterpart. Whether the RST is observed on the session.update
// send (before stream-start is ever emitted) or on the first receive is a
// race the test doesn't pin down — only that it always ends in an error.
func TestTranscriptionModel_DoStream_AbnormalDisconnectSurfacesError(t *testing.T) {
	ts := newOpenAIAbnormalDisconnectTestServer(t)
	defer ts.Close()

	model := newTestTranscriptionModel(ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	for i := 0; i < 5; i++ {
		if _, err = result.Stream.Next(); err != nil {
			return
		}
	}
	t.Fatal("expected an error for an abnormal disconnection, got a silent finish")
}

// newOpenAIAbnormalDisconnectTestServer performs the WebSocket handshake
// itself (rather than golang.org/x/net/websocket's server helper) so it can
// force a TCP RST (via SO_LINGER=0) instead of a clean FIN.
// golang.org/x/net/websocket parses frame headers one byte at a time via
// bufio.Reader.ReadByte, which returns a plain io.EOF for any ordinary
// closed/half-closed connection — indistinguishable, at that layer, from a
// properly received close frame. Only a genuine socket-level error (here,
// "connection reset by peer") is distinct from io.EOF.
func newOpenAIAbnormalDisconnectTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hijacker, ok := w.(stdhttp.Hijacker)
		if !ok {
			t.Fatalf("ResponseWriter does not support hijacking")
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}

		key := r.Header.Get("Sec-WebSocket-Key")
		accept := openAIComputeWebSocketAccept(key)
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := buf.WriteString(resp); err != nil {
			conn.Close() //nolint:errcheck
			return
		}
		if err := buf.Flush(); err != nil {
			conn.Close() //nolint:errcheck
			return
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// SetLinger(0) makes the following Close() send a RST instead of
			// the normal FIN/close handshake, forcing a real socket error on
			// the client's next read instead of a graceful io.EOF.
			_ = tcpConn.SetLinger(0)
		}
		conn.Close() //nolint:errcheck
	}
	return httptest.NewServer(stdhttp.HandlerFunc(handler))
}

// openAIComputeWebSocketAccept computes the Sec-WebSocket-Accept header value
// for a given Sec-WebSocket-Key, per RFC 6455 section 1.3.
func openAIComputeWebSocketAccept(key string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec // required by the WebSocket handshake spec, not for security
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
