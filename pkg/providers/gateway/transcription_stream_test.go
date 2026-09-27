package gateway

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

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

func (s *chanTestAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// gatewayTranscriptionTestServer implements the server side of the
// transcription-stream envelope for tests.
type gatewayTranscriptionTestServer struct {
	ts *httptest.Server

	mu             sync.Mutex
	receivedFrames []map[string]interface{}
	receivedAudio  [][]byte
	protocolHeader string
	requestHeaders stdhttp.Header

	toSend    chan interface{}
	closeConn chan struct{}
}

func newGatewayTranscriptionTestServer(t *testing.T) *gatewayTranscriptionTestServer {
	t.Helper()
	s := &gatewayTranscriptionTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
	handlerFn := func(conn *websocket.Conn) {
		s.mu.Lock()
		s.protocolHeader = conn.Request().Header.Get("Sec-Websocket-Protocol")
		s.requestHeaders = conn.Request().Header.Clone()
		s.mu.Unlock()

		// Closes the underlying connection on demand (simulating the server
		// closing after an error part) without shutting down the whole
		// httptest.Server, which would otherwise block on this hijacked
		// connection's still-blocked read.
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
	handler := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *stdhttp.Request) error {
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: handlerFn,
	}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *gatewayTranscriptionTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

// closeConnection closes just the active WebSocket connection, simulating
// the server closing after an error part without the finish part.
func (s *gatewayTranscriptionTestServer) closeConnection() {
	close(s.closeConn)
}

func (s *gatewayTranscriptionTestServer) waitForFrame(t *testing.T, wantType string, timeout time.Duration) map[string]interface{} {
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

func newTestGatewayTranscriptionModel(t *testing.T, baseURL string) *TranscriptionModel {
	t.Helper()
	p, err := New(Config{APIKey: "test-token", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return NewTranscriptionModel(p, "openai/gpt-realtime-whisper")
}

// TestTranscriptionModel_DoStream_NegotiatesSubprotocolAndSendsEnvelope
// verifies the ai-gateway-transcription.v1 subprotocol handshake and the
// start/audio/audio-done envelope frames.
func TestTranscriptionModel_DoStream_NegotiatesSubprotocolAndSendsEnvelope(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	rate := 16000
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream([]byte("hello-audio")),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)
	server.waitForFrame(t, transcriptionStreamAudioDoneFrameType, time.Second)

	server.mu.Lock()
	protocolHeader := server.protocolHeader
	audio := append([][]byte(nil), server.receivedAudio...)
	server.mu.Unlock()

	if !strings.Contains(protocolHeader, GatewayTranscriptionSubprotocol) {
		t.Errorf("Sec-WebSocket-Protocol = %q, want containing %q", protocolHeader, GatewayTranscriptionSubprotocol)
	}
	if !strings.Contains(protocolHeader, GatewayAuthSubprotocolPrefix+"test-token") {
		t.Errorf("Sec-WebSocket-Protocol = %q, want containing the auth token", protocolHeader)
	}
	if len(audio) != 1 || string(audio[0]) != "hello-audio" {
		t.Fatalf("received audio = %v, want one frame with 'hello-audio'", audio)
	}
}

// TestTranscriptionModel_DoStream_SendsAuthHeaders verifies the WS handshake
// carries auth on both channels, matching TS createAuthHeaders: the raw
// Authorization/ai-gateway-auth-method headers alongside the
// ai-gateway-auth.<token> subprotocol already covered by the negotiation
// test above.
func TestTranscriptionModel_DoStream_SendsAuthHeaders(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)

	server.mu.Lock()
	headers := server.requestHeaders
	server.mu.Unlock()

	if got := headers.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
	}
	if got := headers.Get("ai-gateway-auth-method"); got != "api-key" {
		t.Errorf("ai-gateway-auth-method header = %q, want %q", got, "api-key")
	}
}

// TestTranscriptionModel_DoStream_StreamsDeltasAndFinish mirrors the shared
// TS transcription-stream envelope round trip (stream-start, deltas, final,
// finish -> clean close).
func TestTranscriptionModel_DoStream_StreamsDeltasAndFinish(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)

	server.toSend <- map[string]interface{}{"type": "stream-start", "warnings": []interface{}{}}
	server.toSend <- map[string]interface{}{"type": "transcript-delta", "id": "item-1", "delta": "Hel"}
	server.toSend <- map[string]interface{}{"type": "transcript-delta", "id": "item-1", "delta": "lo"}
	server.toSend <- map[string]interface{}{"type": "transcript-final", "id": "item-1", "text": "Hello"}
	server.toSend <- map[string]interface{}{
		"type": "finish", "text": "Hello",
		"segments": []map[string]interface{}{{"text": "Hello", "startSecond": 0.0, "endSecond": 1.0}},
		"language": "en",
	}

	var parts []*provider.TranscriptionStreamPart
	for {
		p, err := result.Stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Stream.Next() error = %v", err)
		}
		parts = append(parts, p)
	}

	if len(parts) != 5 {
		t.Fatalf("len(parts) = %d, want 5; got %+v", len(parts), parts)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello" || finish.Language != "en" {
		t.Fatalf("finish = %+v", finish)
	}
	if len(finish.Segments) != 1 || finish.Segments[0].Text != "Hello" {
		t.Fatalf("finish.Segments = %+v", finish.Segments)
	}
}

// TestTranscriptionModel_DoStream_SurfacesServerErrorOnClose verifies that an
// `error` part followed by a non-1000 close surfaces as a Stream.Next() error
// (TS: the terminal close error is built from the last error part).
func TestTranscriptionModel_DoStream_SurfacesServerErrorOnClose(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "rate limited", "type": "rate_limit_exceeded"}}

	// drain the error part itself (streamed, not terminal by itself)
	part, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (error part) error = %v", err)
	}
	if part.Type != provider.TranscriptionStreamPartTypeError {
		t.Fatalf("part.Type = %s, want error", part.Type)
	}

	// Now close the server connection without a finish part.
	server.closeConnection()

	_, err = result.Stream.Next()
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Stream.Next() error = %v, want containing 'rate limited'", err)
	}
}

// TestTranscriptionModel_DoStream_StopsAudioOnServerError verifies envelope
// rule 5 (TS gateway-transcription-model.ts stopAudio()): once a server
// `error` part arrives, the caller's audio stream is cancelled instead of
// continuing to send audio while the server holds the connection open.
func TestTranscriptionModel_DoStream_StopsAudioOnServerError(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	// A never-closed channel: pumpAudio would block waiting for the next
	// chunk forever unless something actively stops it.
	audio := &chanTestAudioStream{chunks: make(chan []byte)}

	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "rate limited"}}

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (error part) error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if audio.wasCancelled() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the audio stream to be cancelled after a server error part")
}
