package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

// Ported from
// ai/packages/google-vertex/src/gemini-transcription/google-vertex-gemini-transcription-model.test.ts
// `describe('doStream', ...)`, adapted for Vertex's Bearer-token WS auth,
// LlmBidiService/BidiGenerateContent path, and fully-qualified
// setup.model resource name (vs. the Developer API's relative "models/{id}"
// and x-goog-api-key query auth used by pkg/providers/google's sibling
// test). The harness types below intentionally duplicate
// pkg/providers/google/transcription_stream_test.go's (unexported, so not
// importable) chanAudioStream / liveTranscriptionTestServer.

type chanAudioStream struct {
	chunks chan []byte

	mu           sync.Mutex
	cancelled    bool
	cancelReason error
	cancelCount  int
}

func newChanAudioStream(chunks ...[]byte) *chanAudioStream {
	ch := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &chanAudioStream{chunks: ch}
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
	s.cancelReason = reason
	s.cancelCount++
}

// liveTranscriptionTestServer runs a minimal Vertex Live WebSocket endpoint:
// it records every received client message and the handshake headers, and
// lets the test script server->client events over a channel.
type liveTranscriptionTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}

	handshakeHeaders stdhttp.Header

	toSend    chan interface{}
	closeConn chan struct{}
}

func newLiveTranscriptionTestServer(t *testing.T) *liveTranscriptionTestServer {
	t.Helper()
	s := &liveTranscriptionTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
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
		Handshake: func(_ *websocket.Config, req *stdhttp.Request) error {
			s.mu.Lock()
			s.handshakeHeaders = req.Header.Clone()
			s.mu.Unlock()
			return nil
		},
		Handler: wsHandlerFn,
	}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *liveTranscriptionTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *liveTranscriptionTestServer) all() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]interface{}, len(s.received))
	copy(out, s.received)
	return out
}

func (s *liveTranscriptionTestServer) authorizationHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handshakeHeaders == nil {
		return ""
	}
	return s.handshakeHeaders.Get("Authorization")
}

func (s *liveTranscriptionTestServer) userAgentHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handshakeHeaders == nil {
		return ""
	}
	return s.handshakeHeaders.Get("User-Agent")
}

func (s *liveTranscriptionTestServer) waitFor(t *testing.T, pred func(map[string]interface{}) bool, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, m := range s.received {
			if pred(m) {
				s.mu.Unlock()
				return m
			}
		}
		s.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a matching message; got %v", s.all())
	return nil
}

func hasRealtimeInputKey(key string) func(map[string]interface{}) bool {
	return func(m map[string]interface{}) bool {
		realtimeInput, ok := m["realtimeInput"].(map[string]interface{})
		if !ok {
			return false
		}
		_, ok = realtimeInput[key]
		return ok
	}
}

func newLiveTestModel(t *testing.T, baseURL string) *GeminiTranscriptionModel {
	t.Helper()
	prov := newVertexTestProvider(t, baseURL)
	return NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)
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

func TestGeminiTranscriptionModel_DoStream_RejectsNonLiveModel(t *testing.T) {
	prov := newVertexTestProvider(t, "http://example.invalid")
	model := NewGeminiTranscriptionModel(prov, ModelGemini35Transcribe)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err == nil {
		t.Fatal("expected an error for a non-live model ID")
	}
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) {
		t.Fatalf("err = %v (%T), want *InvalidArgumentError", err, err)
	}
	if !strings.Contains(err.Error(), "does not support streaming transcription") {
		t.Fatalf("err = %v", err)
	}
}

func TestGeminiTranscriptionModel_DoStream_RejectsNon16kHzPCM(t *testing.T) {
	prov := newVertexTestProvider(t, "http://example.invalid")
	model := NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)

	rate24k := 24000
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate24k},
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 16kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 16kHz PCM message", err)
	}

	_, err = model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/wav"},
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 16kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 16kHz PCM message for non-pcm type", err)
	}
}

// TestGeminiTranscriptionModel_DoStream_StreamsTranscriptEndToEnd mirrors the
// TS "streams transcription over the Vertex Live API WebSocket" test,
// additionally verifying the Bearer Authorization handshake header and the
// fully-qualified setup.model resource path (Vertex-specific; the Developer
// API sibling model uses a relative "models/{id}" path).
func TestGeminiTranscriptionModel_DoStream_StreamsTranscriptEndToEnd(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	audio := newChanAudioStream([]byte{1, 2, 3})

	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"customVocabulary": []interface{}{"Gemini"},
				"languageCodes":    []interface{}{"en-US"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	setupMsg := server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	setup, _ := setupMsg["setup"].(map[string]interface{})
	wantModel := "projects/test-project/locations/us-central1/publishers/google/models/" + ModelGemini35TranscribeLive
	if setup["model"] != wantModel {
		t.Fatalf("setup.model = %v, want %v", setup["model"], wantModel)
	}
	inputAudioTranscription, _ := setup["inputAudioTranscription"].(map[string]interface{})
	if inputAudioTranscription == nil {
		t.Fatal("setup.inputAudioTranscription missing")
	}
	if langs, _ := inputAudioTranscription["languageCodes"].([]interface{}); len(langs) != 1 || langs[0] != "en-US" {
		t.Fatalf("inputAudioTranscription.languageCodes = %v", inputAudioTranscription["languageCodes"])
	}
	if vocab, _ := inputAudioTranscription["customVocabulary"].([]interface{}); len(vocab) != 1 || vocab[0] != "Gemini" {
		t.Fatalf("inputAudioTranscription.customVocabulary = %v", inputAudioTranscription["customVocabulary"])
	}

	if got := server.authorizationHeader(); got != "Bearer test-oauth-token" {
		t.Fatalf("handshake Authorization header = %q, want %q", got, "Bearer test-oauth-token")
	}
	// TS google-vertex-gemini-transcription-model.ts reuses this.config.headers()
	// -- the same tagged getHeaders() closure used for REST calls -- for the
	// WebSocket handshake, so it carries the ai-sdk-google-vertex/VERSION tag too.
	if got := server.userAgentHeader(); !strings.HasPrefix(got, "ai-sdk-google-vertex/") {
		t.Fatalf("handshake User-Agent header = %q, want ai-sdk-google-vertex/... prefix", got)
	}

	// audio is gated on setupComplete: nothing else should have been sent yet.
	if got := server.all(); len(got) != 1 {
		t.Fatalf("received before setupComplete = %+v, want only the setup message", got)
	}

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}

	appendMsg := server.waitFor(t, hasRealtimeInputKey("audio"), time.Second)
	realtimeInput := appendMsg["realtimeInput"].(map[string]interface{})
	audioPart := realtimeInput["audio"].(map[string]interface{})
	if mimeType, _ := audioPart["mimeType"].(string); mimeType != "audio/pcm;rate=16000" {
		t.Fatalf("mimeType = %q", mimeType)
	}
	if dataB64, _ := audioPart["data"].(string); dataB64 == "" {
		t.Fatal("expected base64 audio data")
	} else if decoded, err := base64.StdEncoding.DecodeString(dataB64); err != nil || string(decoded) != "\x01\x02\x03" {
		t.Fatalf("decoded audio = %q, err = %v", decoded, err)
	}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interimInputTranscription": map[string]interface{}{"text": "hel"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "hello ", "languageCode": "en-US"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "world.", "finished": true}}}
	server.toSend <- map[string]interface{}{
		"usageMetadata": map[string]interface{}{"promptTokenCount": 7},
		"serverContent": map[string]interface{}{"turnComplete": true, "interactionStatus": "IDLE"},
	}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	parts := append([]*provider.TranscriptionStreamPart{first}, rest...)
	if len(parts) != 6 {
		t.Fatalf("len(parts) = %d, want 6; got %+v", len(parts), parts)
	}
	if parts[0].Type != provider.TranscriptionStreamPartTypeStreamStart || len(parts[0].Warnings) != 0 {
		t.Fatalf("parts[0] = %+v", parts[0])
	}
	if parts[1].Type != provider.TranscriptionStreamPartTypePartial || parts[1].ID != "google-segment-0" || parts[1].Text != "hel" {
		t.Fatalf("parts[1] = %+v", parts[1])
	}
	if parts[2].Type != provider.TranscriptionStreamPartTypeDelta || parts[2].Delta != "hello " {
		t.Fatalf("parts[2] = %+v", parts[2])
	}
	if parts[3].Type != provider.TranscriptionStreamPartTypeDelta || parts[3].Delta != "world." {
		t.Fatalf("parts[3] = %+v", parts[3])
	}
	if parts[4].Type != provider.TranscriptionStreamPartTypeFinal || parts[4].Text != "hello world." {
		t.Fatalf("parts[4] = %+v", parts[4])
	}
	finish := parts[5]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "hello world." || finish.Language != "en-US" {
		t.Fatalf("finish = %+v", finish)
	}
	wantMeta := map[string]interface{}{"google": map[string]interface{}{"usageMetadata": map[string]interface{}{"promptTokenCount": float64(7)}}}
	gotMeta, _ := json.Marshal(finish.ProviderMetadata)
	wantMetaJSON, _ := json.Marshal(wantMeta)
	if string(gotMeta) != string(wantMetaJSON) {
		t.Fatalf("providerMetadata = %s, want %s", gotMeta, wantMetaJSON)
	}
	// Ported from TS google-vertex-gemini-transcription-model.test.ts
	// doStream "should surface usage" (TS 8c659885c5 / #21427).
	if got := finish.Usage["promptTokenCount"]; got != float64(7) {
		t.Fatalf("finish.Usage[promptTokenCount] = %v, want 7", got)
	}
}

// TestGeminiTranscriptionModel_DoStream_PassesSmartModeIntoLiveSetup mirrors
// the TS "passes the SMART transcription mode into the live setup".
func TestGeminiTranscriptionModel_DoStream_PassesSmartModeIntoLiveSetup(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"mode": "SMART"},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	setupMsg := server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	setup, _ := setupMsg["setup"].(map[string]interface{})
	inputAudioTranscription, _ := setup["inputAudioTranscription"].(map[string]interface{})
	if inputAudioTranscription == nil || inputAudioTranscription["mode"] != "SMART" {
		t.Fatalf("setup.inputAudioTranscription = %+v, want mode=SMART", inputAudioTranscription)
	}
	if len(inputAudioTranscription) != 1 {
		t.Fatalf("setup.inputAudioTranscription = %+v, want only mode set", inputAudioTranscription)
	}

	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "hi", "finished": true}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interactionStatus": "IDLE"}}

	if _, err := drainUntilFinishOrError(t, result.Stream); err != nil {
		t.Fatalf("stream error = %v", err)
	}
}
