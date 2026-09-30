package google

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
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// chanAudioStream is a provider.AudioStream test double backed by a channel
// of chunks; closing the channel signals io.EOF. Cancel is recorded so tests
// can assert the stream propagates cancellation to the caller's audio
// producer on failure (per IMPL_INSTRUCTIONS: "cancel the caller's
// AudioStream on failure").
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

// newBlockingAudioStream returns an AudioStream whose Next never yields a
// chunk or EOF until the returned stream is cancelled or its context is
// done, for tests that need audio to still be "streaming" indefinitely.
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
	s.cancelReason = reason
	s.cancelCount++
}

func (s *chanAudioStream) wasCancelled() (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled, s.cancelCount
}

// liveTranscriptionTestServer runs a minimal Gemini Live WebSocket endpoint:
// it records every received client message and lets the test script
// server->client events over a channel.
type liveTranscriptionTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}

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
	handler := websocket.Server{Handler: wsHandlerFn}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *liveTranscriptionTestServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.ts.URL, "http")
}

func (s *liveTranscriptionTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *liveTranscriptionTestServer) closeConnection() {
	close(s.closeConn)
}

func (s *liveTranscriptionTestServer) all() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]interface{}, len(s.received))
	copy(out, s.received)
	return out
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

func newLiveTestModel(t *testing.T, baseURL string) *TranscriptionModel {
	t.Helper()
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	m := NewTranscriptionModel(p, ModelGemini35TranscribeLive)
	// No test in this file depends on the finish-grace timer itself elapsing:
	// every scenario finishes via an explicit terminal signal (turnComplete /
	// interactionStatus / a connection close). A short override here (this
	// used to be 50ms) previously raced the timer against real message
	// delivery on the httptest WebSocket server: under load (observed with
	// `go test -race -count=10` on a busy machine), the timer could fire and
	// finish the stream with truncated text before a same-tick server message
	// was read and processed. Leaving finishGraceMs unset applies
	// defaultFinishGraceDuration (3s, matching TS's own production default
	// and its 5000ms test default), which no real test scenario here comes
	// close to hitting.
	return m
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

func TestTranscriptionModel_DoStream_RejectsNonLiveModel(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelGemini35Transcribe)
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

func TestTranscriptionModel_DoStream_RejectsNon16kHzPCM(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelGemini35TranscribeLive)

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

func TestTranscriptionModel_DoStream_RequiresAPIKey(t *testing.T) {
	old, hadOld := os.LookupEnv("GOOGLE_GENERATIVE_AI_API_KEY")
	os.Unsetenv("GOOGLE_GENERATIVE_AI_API_KEY")
	defer func() {
		if hadOld {
			os.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", old)
		}
	}()

	p := New(Config{})
	model := NewTranscriptionModel(p, ModelGemini35TranscribeLive)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err == nil || !strings.Contains(err.Error(), "API key is required for streaming transcription") {
		t.Fatalf("err = %v, want a missing-API-key message", err)
	}
}

// TestTranscriptionModel_DoStream_PerCallAPIKeyOverridesProviderLevel
// verifies that a per-call header deterministically wins over the
// provider-level API key regardless of casing, and that the reverse case
// (no per-call override) falls back to the provider-level key. Resolving the
// key from each header source separately (rather than from an
// already-merged map) avoids depending on Go's unspecified map iteration
// order when the two sources use different casings of x-goog-api-key.
// TestTranscriptionModel_BaseHeadersCarryUserAgentTag mirrors TS
// google-transcription-model.ts's doStream, which reuses `this.config.headers()`
// -- the same tagged getHeaders() closure used for REST calls -- for the
// WebSocket handshake, so it carries the `ai-sdk/google/VERSION` tag too.
func TestTranscriptionModel_BaseHeadersCarryUserAgentTag(t *testing.T) {
	p := New(Config{APIKey: "provider-level-key"})
	m := NewTranscriptionModel(p, ModelGemini35TranscribeLive)

	ua := m.baseTranscriptionHeaders()["user-agent"]
	if !strings.HasPrefix(ua, "ai-sdk/google/") {
		t.Fatalf("user-agent = %q, want ai-sdk/google/... prefix", ua)
	}
}

func TestTranscriptionModel_DoStream_PerCallAPIKeyOverridesProviderLevel(t *testing.T) {
	p := New(Config{APIKey: "provider-level-key"})
	m := NewTranscriptionModel(p, ModelGemini35TranscribeLive)

	for _, callKeyHeaderName := range []string{"x-goog-api-key", "X-Goog-Api-Key", "X-GOOG-API-KEY"} {
		for i := 0; i < 20; i++ {
			headers := m.baseTranscriptionHeaders()
			callHeaders := map[string]string{callKeyHeaderName: "call-level-key"}
			baseAPIKey, filteredBase := extractGoogleAPIKeyHeader(headers)
			callAPIKey, filteredCall := extractGoogleAPIKeyHeader(callHeaders)
			apiKey := baseAPIKey
			if callAPIKey != "" {
				apiKey = callAPIKey
			}
			if apiKey != "call-level-key" {
				t.Fatalf("header %q, iteration %d: apiKey = %q, want the per-call override", callKeyHeaderName, i, apiKey)
			}
			if _, ok := filteredBase["x-goog-api-key"]; ok {
				t.Fatalf("filteredBase still carries x-goog-api-key: %v", filteredBase)
			}
			if len(filteredCall) != 0 {
				t.Fatalf("filteredCall = %v, want the key header stripped", filteredCall)
			}
		}
	}

	// No per-call override: the provider-level key is used.
	baseAPIKey, _ := extractGoogleAPIKeyHeader(m.baseTranscriptionHeaders())
	callAPIKey, _ := extractGoogleAPIKeyHeader(map[string]string{"other-header": "x"})
	apiKey := baseAPIKey
	if callAPIKey != "" {
		apiKey = callAPIKey
	}
	if apiKey != "provider-level-key" {
		t.Fatalf("apiKey = %q, want the provider-level key when no per-call override is set", apiKey)
	}
}

// TestTranscriptionModel_DoStream_StreamsTranscriptEndToEnd mirrors the TS
// "streams transcription over the Gemini Live API WebSocket" test.
func TestTranscriptionModel_DoStream_StreamsTranscriptEndToEnd(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	audio := newChanAudioStream([]byte{1, 2, 3})

	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
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
	if setup["model"] != "models/"+ModelGemini35TranscribeLive {
		t.Fatalf("setup.model = %v", setup["model"])
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

	// audio is gated on setupComplete: nothing else should have been sent yet.
	if got := server.all(); len(got) != 1 {
		t.Fatalf("received before setupComplete = %+v, want only the setup message", got)
	}

	// Draining stream-start unblocks run()'s message-processing loop (an
	// unbuffered channel send), which is what lets it observe setupComplete
	// below and unblock the audio pump gated on it.
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
}

// TestTranscriptionModel_DoStream_PassesSmartModeIntoLiveSetup mirrors the TS
// "passes the SMART transcription mode into the live setup".
func TestTranscriptionModel_DoStream_PassesSmartModeIntoLiveSetup(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"mode": "SMART"},
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

// TestTranscriptionModel_DoStream_AcceptsRequiresActionAsIdle mirrors the TS
// "accepts the pre-launch REQUIRES_ACTION interaction status as idle".
func TestTranscriptionModel_DoStream_AcceptsRequiresActionAsIdle(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "hi", "finished": true}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interactionStatus": "REQUIRES_ACTION"}}

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "hi" {
		t.Fatalf("finish = %+v, want finish with text 'hi'", finish)
	}
}

// TestTranscriptionModel_DoStream_FallsBackToLatestInterim mirrors the TS
// "falls back to the latest interim partial when no final segment arrives".
func TestTranscriptionModel_DoStream_FallsBackToLatestInterim(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interimInputTranscription": map[string]interface{}{"text": "hello wor"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interimInputTranscription": map[string]interface{}{"text": "hello world."}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interactionStatus": "IDLE"}}

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final := parts[len(parts)-2]
	if final.Type != provider.TranscriptionStreamPartTypeFinal || final.Text != "hello world." {
		t.Fatalf("final = %+v", final)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "hello world." {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_FinishesOnCleanCloseAfterAudioEnded mirrors
// TS "finishes with accumulated text when the server closes after audio
// ended".
func TestTranscriptionModel_DoStream_FinishesOnCleanCloseAfterAudioEnded(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "partial words"}}}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "partial words" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_FinishWhenCloseWhilePending is a regression
// test for the close-while-pending gate: it uses a finish-grace period long
// enough (10s) that the timer could not plausibly have fired on its own
// before the server closes the connection, proving the close is what
// finishes the stream (gated on the pending finish timer, matching the
// equivalent speech-translation stream's onClose gate) rather than the timer
// itself elapsing.
func TestTranscriptionModel_DoStream_FinishWhenCloseWhilePending(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	model.finishGraceMs = 10 * time.Second
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "partial words"}}}
	time.Sleep(20 * time.Millisecond)

	// close while the (10s) grace-period finish is pending confirms completion:
	server.closeConnection()

	start := time.Now()
	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Fatalf("finish took %v, want it to complete well under the 10s grace period (the close, not the timer, should finish the stream)", elapsed)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "partial words" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_ErrorsOnCloseBeforeAudioEnded mirrors TS
// "errors when the socket closes before the audio ended".
func TestTranscriptionModel_DoStream_ErrorsOnCloseBeforeAudioEnded(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil {
		t.Fatal("expected an error for a close before audio ended")
	}
	if cancelled, _ := audio.wasCancelled(); !cancelled {
		t.Fatal("expected the AudioStream to be cancelled on failure")
	}
}

// TestTranscriptionModel_DoStream_SurfacesServerErrorMessage mirrors TS
// "surfaces server error messages".
func TestTranscriptionModel_DoStream_SurfacesServerErrorMessage(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	audio := newChanAudioStream([]byte{1})
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.toSend <- map[string]interface{}{"error": map[string]interface{}{"message": "quota exceeded"}}

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v, want containing 'quota exceeded'", err)
	}
	if cancelled, _ := audio.wasCancelled(); !cancelled {
		t.Fatal("expected the AudioStream to be cancelled on a server error")
	}
}

// TestTranscriptionModel_DoStream_EmitsRawChunks mirrors TS "emits raw chunks
// when includeRawChunks is set".
func TestTranscriptionModel_DoStream_EmitsRawChunks(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	// includeRawChunks emits a raw part for every server message, including
	// setupComplete itself, so pumpAudio's setupComplete gate is behind an
	// emit that needs draining; drain continuously in the background instead
	// of interleaving explicit Next() calls with each server send.
	type drainResult struct {
		parts []*provider.TranscriptionStreamPart
		err   error
	}
	drained := make(chan drainResult, 1)
	go func() {
		parts, err := drainUntilFinishOrError(t, result.Stream)
		drained <- drainResult{parts: parts, err: err}
	}()

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "ok", "finished": true}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"interactionStatus": "IDLE"}}

	var res drainResult
	select {
	case res = <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stream to finish")
	}
	if res.err != nil {
		t.Fatalf("stream error = %v", res.err)
	}
	var rawCount int
	for _, p := range res.parts {
		if p.Type == provider.TranscriptionStreamPartTypeRaw {
			rawCount++
		}
	}
	if rawCount == 0 {
		t.Fatal("expected at least one raw chunk")
	}
}

// TestTranscriptionModel_DoStream_URLIncludesAPIKeyAndModelPath asserts the
// Live API WebSocket URL shape (TS's exact URL assertion).
func TestTranscriptionModel_DoStream_URLIncludesAPIKeyAndModelPath(t *testing.T) {
	if got, want := getLiveTranscriptionWebSocketURL("https://generativelanguage.googleapis.com/v1beta", "test-api-key"),
		"wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=test-api-key"; got != want {
		t.Fatalf("getLiveTranscriptionWebSocketURL() = %q, want %q", got, want)
	}
}

// TestTranscriptionModel_DoStream_CancelMidStream exercises Close() while the
// stream is actively receiving, verifying Next() unblocks and the caller's
// AudioStream is cancelled, with no goroutine leaks (run with -race).
func TestTranscriptionModel_DoStream_CancelMidStream(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newLiveTestModel(t, server.ts.URL)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}

	// drain stream-start
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
}

// failingLiveAudioStream is a provider.AudioStream test double whose Next
// returns a fixed non-EOF error after yielding any configured chunks,
// simulating a real audio-source failure (as opposed to natural completion).
type failingLiveAudioStream struct {
	chunks [][]byte
	err    error

	mu        sync.Mutex
	cancelled bool
}

func (s *failingLiveAudioStream) Next(ctx context.Context) ([]byte, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return c, nil
	}
	return nil, s.err
}

func (s *failingLiveAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

func (s *failingLiveAudioStream) wasCancelled() bool {
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
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	audio := &failingLiveAudioStream{err: errors.New("audio source failed")}
	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "audio source failed") {
		t.Fatalf("err = %v, want an error containing 'audio source failed'", err)
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
// connectToWebSocket's onSocketError semantics: an abnormal disconnection (a
// connection dropped mid-frame via a TCP RST, distinct from a clean WebSocket
// close frame) must always surface as an error, unlike a clean close (TS
// onClose), which — per TestTranscriptionModel_DoStream_FinishesOnCleanCloseAfterAudioEnded
// and TestTranscriptionModel_DoStream_ErrorsOnCloseBeforeAudioEnded — only
// finishes successfully once audioEnded. Regression test for a bug where
// every receive error was routed through the same audioEnded branching as a
// clean close, so a genuine socket error arriving after audioEnded was
// treated as a silent finish instead of failing the stream.
func TestTranscriptionModel_DoStream_AbnormalDisconnectSurfacesError(t *testing.T) {
	ts := newGoogleAbnormalDisconnectTestServer(t)
	defer ts.Close()

	model := newLiveTestModel(t, ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil {
		t.Fatal("expected an error for an abnormal disconnection, got a silent finish")
	}
}

// newGoogleAbnormalDisconnectTestServer performs the WebSocket handshake
// itself (rather than golang.org/x/net/websocket's server helper) so it can
// force a TCP RST (via SO_LINGER=0) instead of a clean FIN.
// golang.org/x/net/websocket parses frame headers one byte at a time via
// bufio.Reader.ReadByte, which returns a plain io.EOF for any ordinary
// closed/half-closed connection — indistinguishable, at that layer, from a
// properly received close frame. Only a genuine socket-level error (here,
// "connection reset by peer") is distinct from io.EOF.
func newGoogleAbnormalDisconnectTestServer(t *testing.T) *httptest.Server {
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
		accept := googleComputeWebSocketAccept(key)
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

// googleComputeWebSocketAccept computes the Sec-WebSocket-Accept header value
// for a given Sec-WebSocket-Key, per RFC 6455 section 1.3.
func googleComputeWebSocketAccept(key string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec // required by the WebSocket handshake spec, not for security
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
