package gateway

import (
	"bufio"
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
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
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

// TestTranscriptionModel_DoStream_AbnormalDisconnectAfterServerErrorSurfacesGenericError
// mirrors TS's real event ordering: onSocketError (an abnormal disconnect,
// e.g. a TCP RST) always finishes the stream with the generic "Connection
// error on AI Gateway transcription stream" message, even if a server
// `error` part was received first — because in TS, onSocketError fires (and
// finishes the stream) before onClose's hasServerErrorPart branch can run.
// A regression test for a bug where the Go port checked hasServerError
// before checking whether the close was clean, so an abnormal disconnect
// after an error part incorrectly surfaced the buffered server error instead
// of the generic connection error.
func TestTranscriptionModel_DoStream_AbnormalDisconnectAfterServerErrorSurfacesGenericError(t *testing.T) {
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
		accept := gatewayTestComputeWebSocketAccept(key)
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

		// Wait for the client's "start" frame so the error frame below is
		// written only after the client has actually completed the dial and
		// begun reading — otherwise the RST below can race ahead of the
		// client's own handshake completion.
		if _, err := gatewayTestReadClientFrame(buf.Reader); err != nil {
			conn.Close() //nolint:errcheck
			return
		}

		errorFrame := gatewayTestTextFrame([]byte(`{"type":"error","error":{"message":"rate limited"}}`))
		if _, err := conn.Write(errorFrame); err != nil {
			conn.Close() //nolint:errcheck
			return
		}

		// Wait for the client's "audio-done" frame (its empty AudioStream
		// hits EOF immediately after "start"): by the time this arrives, the
		// error frame written above has necessarily already been delivered
		// to the client's kernel socket buffer (same TCP stream, FIFO
		// ordered), so the RST below cannot race ahead of it.
		if _, err := gatewayTestReadClientFrame(buf.Reader); err != nil {
			conn.Close() //nolint:errcheck
			return
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// SetLinger(0) makes the following Close() send a RST instead of
			// a normal FIN/close handshake, forcing a real socket error on
			// the client's next read instead of a graceful io.EOF.
			_ = tcpConn.SetLinger(0)
		}
		conn.Close() //nolint:errcheck
	}
	ts := httptest.NewServer(stdhttp.HandlerFunc(handler))
	defer ts.Close()

	model := newTestGatewayTranscriptionModel(t, ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	part, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (error part) error = %v", err)
	}
	if part.Type != provider.TranscriptionStreamPartTypeError {
		t.Fatalf("part.Type = %s, want error", part.Type)
	}

	_, err = result.Stream.Next()
	if err == nil {
		t.Fatal("Stream.Next() error = nil, want the abnormal-disconnect error")
	}
	if strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Stream.Next() error = %v, want the generic connection error, not the buffered server error", err)
	}
	if !strings.Contains(err.Error(), "Connection error on AI Gateway transcription stream") {
		t.Fatalf("Stream.Next() error = %v, want containing %q", err, "Connection error on AI Gateway transcription stream")
	}
}

// gatewayTestReadClientFrame reads and unmasks one client->server RFC 6455
// frame (client frames are always masked), returning its payload. It only
// supports the small single-frame, short-payload (<126 bytes) messages this
// test's client sends (JSON "start"/"audio-done" control frames).
func gatewayTestReadClientFrame(r *bufio.Reader) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	payloadLen := int(header[1] &^ 0x80)
	switch {
	case payloadLen == 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payloadLen = int(ext[0])<<8 | int(ext[1])
	case payloadLen == 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payloadLen = int(ext[7])
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(r, mask); err != nil {
		return nil, err
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return payload, nil
}

// gatewayTestTextFrame builds a minimal unmasked RFC 6455 text frame (server
// frames are never masked) carrying payload.
func gatewayTestTextFrame(payload []byte) []byte {
	frame := []byte{0x81} // FIN + text opcode
	n := len(payload)
	switch {
	case n < 126:
		frame = append(frame, byte(n))
	case n < 65536:
		frame = append(frame, 126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(frame, payload...)
}

// gatewayTestComputeWebSocketAccept computes the Sec-WebSocket-Accept header
// value for a given Sec-WebSocket-Key, per RFC 6455 section 1.3.
func gatewayTestComputeWebSocketAccept(key string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec // required by the WebSocket handshake spec, not for security
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// TestTranscriptionModel_DoStream_TypedServerError verifies that a server
// `error` part with a recognized `type` maps to the matching typed Gateway
// error class (TS createErrorFromServerErrorPart), not just a generic error
// wrapping the message text.
func TestTranscriptionModel_DoStream_TypedServerError(t *testing.T) {
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

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (error part) error = %v", err)
	}
	server.closeConnection()

	_, err = result.Stream.Next()
	var rateLimitErr *gatewayerrors.GatewayRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("Stream.Next() error = %v (%T), want *GatewayRateLimitError", err, err)
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

// failingTestAudioStream is a provider.AudioStream test double whose Next
// returns a fixed non-EOF error after yielding any configured chunks,
// simulating a real audio-source failure (as opposed to natural completion).
type failingTestAudioStream struct {
	chunks [][]byte
	err    error

	mu        sync.Mutex
	cancelled bool
}

func (s *failingTestAudioStream) Next(ctx context.Context) ([]byte, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return c, nil
	}
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
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	audio := &failingTestAudioStream{err: errors.New("audio source failed")}
	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	_, err = result.Stream.Next()
	for err == nil {
		_, err = result.Stream.Next()
	}
	if !strings.Contains(err.Error(), "audio source failed") {
		t.Fatalf("err = %v, want an error containing 'audio source failed'", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if audio.wasCancelled() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the AudioStream to be cancelled when the audio read fails")
}

// TestTranscriptionModel_DoStream_TeamHeaderPerCallOverride verifies the
// x-vercel-ai-gateway-team subprotocol scope comes from the per-call merged
// header set (config headers + opts.Headers), not the provider's static
// TeamIDOrSlug field, matching TS getProtocolsFromHeaders (which reads the
// team scope out of the already-combined header set).
func TestTranscriptionModel_DoStream_TeamHeaderPerCallOverride(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()

	p, err := New(Config{APIKey: "test-token", BaseURL: server.ts.URL, TeamIDOrSlug: "default-team"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model := NewTranscriptionModel(p, "openai/gpt-realtime-whisper")

	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Headers:          map[string]string{"x-vercel-ai-gateway-team": "override-team"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)

	server.mu.Lock()
	protocolHeader := server.protocolHeader
	server.mu.Unlock()

	wantTeamProtocol := GatewayTeamSubprotocolPrefix + encodeSubprotocolValue("override-team")
	if strings.Contains(protocolHeader, GatewayTeamSubprotocolPrefix+encodeSubprotocolValue("default-team")) {
		t.Errorf("Sec-WebSocket-Protocol = %q, still carries the static config team, want the per-call override", protocolHeader)
	}
	if !strings.Contains(protocolHeader, wantTeamProtocol) {
		t.Errorf("Sec-WebSocket-Protocol = %q, want containing %q", protocolHeader, wantTeamProtocol)
	}
}

// TestParseGatewayTranscriptionStreamPart_RejectsMalformedFrames mirrors TS
// provider-utils transcription-stream-envelope.test.ts: a recognized `type`
// with a missing/wrong-typed required field is rejected wholesale (ok=false)
// rather than silently defaulting the bad field to its zero value.
func TestParseGatewayTranscriptionStreamPart_RejectsMalformedFrames(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"finish missing text", `{"type":"finish","segments":[]}`},
		{"finish text wrong type", `{"type":"finish","text":123,"segments":[]}`},
		{"finish segments not array", `{"type":"finish","text":"hi","segments":"nope"}`},
		{"finish segment missing startSecond", `{"type":"finish","text":"hi","segments":[{"text":"hi","endSecond":1}]}`},
		{"transcript-delta missing delta", `{"type":"transcript-delta","id":"item-1"}`},
		{"transcript-partial missing text", `{"type":"transcript-partial","id":"item-1"}`},
		{"transcript-final missing text", `{"type":"transcript-final","id":"item-1"}`},
		{"stream-start warnings not array", `{"type":"stream-start","warnings":"nope"}`},
		{"stream-start warning missing type", `{"type":"stream-start","warnings":[{"message":"x"}]}`},
		{"raw missing rawValue", `{"type":"raw"}`},
		{"error missing error", `{"type":"error"}`},
		{"response-metadata bad timestamp", `{"type":"response-metadata","timestamp":"not-a-date"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseGatewayTranscriptionStreamPart(tc.json); ok {
				t.Fatalf("parseGatewayTranscriptionStreamPart(%q) = ok, want rejected", tc.json)
			}
		})
	}
}

// TestParseGatewayTranscriptionStreamPart_AcceptsValidFrames is the
// complement of the rejection test above: well-formed frames for every part
// type still parse successfully.
func TestParseGatewayTranscriptionStreamPart_AcceptsValidFrames(t *testing.T) {
	cases := []string{
		`{"type":"stream-start","warnings":[{"type":"other","message":"x"}]}`,
		`{"type":"transcript-delta","id":"item-1","delta":"Hel"}`,
		`{"type":"transcript-partial","id":"item-1","text":"Hel"}`,
		`{"type":"transcript-final","id":"item-1","text":"Hello"}`,
		`{"type":"finish","text":"Hello","segments":[{"text":"Hello","startSecond":0,"endSecond":1}]}`,
		`{"type":"response-metadata","modelId":"m","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"raw","rawValue":{"a":1}}`,
		`{"type":"error","error":{"message":"boom"}}`,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			if _, ok := parseGatewayTranscriptionStreamPart(tc); !ok {
				t.Fatalf("parseGatewayTranscriptionStreamPart(%q) = rejected, want ok", tc)
			}
		})
	}
}
