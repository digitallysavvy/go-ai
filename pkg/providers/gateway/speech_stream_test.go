package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// speechTestText is a provider.SpeechTextStream over a fixed list of parts
// that records its cancellation.
type speechTestText struct {
	mu        sync.Mutex
	parts     []provider.SpeechStreamTextPart
	i         int
	block     bool
	cancelled chan struct{}
	once      sync.Once
}

func newSpeechTestText(parts ...provider.SpeechStreamTextPart) *speechTestText {
	return &speechTestText{parts: parts, cancelled: make(chan struct{})}
}

func (s *speechTestText) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
	s.mu.Lock()
	if s.i < len(s.parts) {
		p := s.parts[s.i]
		s.i++
		s.mu.Unlock()
		return &p, nil
	}
	block := s.block
	s.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, io.EOF
}

func (s *speechTestText) Cancel(error) { s.once.Do(func() { close(s.cancelled) }) }

func texts(ss ...string) *speechTestText {
	parts := make([]provider.SpeechStreamTextPart, len(ss))
	for i, s := range ss {
		parts[i] = provider.SpeechStreamTextPart{Text: s}
	}
	return newSpeechTestText(parts...)
}

func newTestGatewaySpeechModel(t *testing.T, baseURL string) *SpeechModel {
	t.Helper()
	p, err := New(Config{APIKey: "test-token", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return NewSpeechModel(p, "elevenlabs/eleven-flash-v2.5")
}

func (s *gatewayTranscriptionTestServer) frames() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]interface{}(nil), s.receivedFrames...)
}

func TestSpeechModel_DoStreamSpeech_SpeaksTheSpeechStreamEnvelope(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)

	rate := 24000
	speed := 1.1
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text: newSpeechTestText(
			provider.SpeechStreamTextPart{Text: "Hello "},
			provider.SpeechStreamTextPart{Text: "world", Voice: "voice-2", ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}},
		),
		Voice:           "voice-1",
		OutputFormat:    &provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		Speed:           &speed,
		Language:        "en",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"seed": float64(7)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForFrame(t, "speech-stream.text-done", 2*time.Second)
	server.mu.Lock()
	protocols := server.protocolHeader
	server.mu.Unlock()
	if !strings.Contains(protocols, "ai-gateway-speech.v1") || !strings.Contains(protocols, "ai-gateway-auth.test-token") {
		t.Fatalf("Sec-WebSocket-Protocol = %q", protocols)
	}
	want := []map[string]interface{}{
		{"type": "speech-stream.start", "voice": "voice-1", "outputFormat": map[string]interface{}{"type": "audio/pcm", "rate": float64(24000)},
			"speed": 1.1, "language": "en", "providerOptions": map[string]interface{}{"elevenlabs": map[string]interface{}{"seed": float64(7)}}},
		{"type": "speech-stream.text-delta", "delta": "Hello "},
		{"type": "speech-stream.text-delta", "delta": "world", "voice": "voice-2", "providerOptions": map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}},
		{"type": "speech-stream.text-done"},
	}
	if got := server.frames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("frames = %#v\nwant %#v", got, want)
	}
	if result.Response.ModelID != "elevenlabs/eleven-flash-v2.5" {
		t.Fatalf("response = %+v", result.Response)
	}

	server.toSend <- map[string]interface{}{"type": "stream-start", "warnings": []interface{}{}}
	server.toSend <- map[string]interface{}{"type": "audio", "audio": "AQI=", "audioFormat": map[string]interface{}{"type": "audio/mpeg", "rate": 44100}}
	server.toSend <- map[string]interface{}{"type": "unknown-part"}
	server.toSend <- map[string]interface{}{"type": "finish", "usage": map[string]interface{}{"inputCharacters": 11}, "providerMetadata": map[string]interface{}{"gateway": map[string]interface{}{"cost": "0.00044"}}}

	var types []string
	for {
		part, err := result.Stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		types = append(types, part.Type)
		switch part.Type {
		case "audio":
			if !reflect.DeepEqual(part.Audio, []byte{1, 2}) || part.AudioFormat.Type != "audio/mpeg" || *part.AudioFormat.Rate != 44100 {
				t.Fatalf("audio part = %+v", part)
			}
		case "finish":
			if *part.Usage.InputCharacters != 11 {
				t.Fatalf("usage = %+v", part.Usage)
			}
		}
	}
	if strings.Join(types, ",") != "stream-start,audio,finish" {
		t.Fatalf("types = %v", types)
	}
}

func TestSpeechModel_DoStreamSpeech_SplitPartKeepsVoiceOnEveryFrameAndOptionsOnFirst(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)

	long := strings.Repeat("x", 70*1024)
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text: newSpeechTestText(provider.SpeechStreamTextPart{Text: long, Voice: "voice-2",
			ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.text-done", 5*time.Second)

	var deltas []map[string]interface{}
	for _, f := range server.frames() {
		if f["type"] == "speech-stream.text-delta" {
			deltas = append(deltas, f)
		}
	}
	if len(deltas) < 2 {
		t.Fatalf("expected the part to be split, got %d frames", len(deltas))
	}
	var joined strings.Builder
	for i, f := range deltas {
		if f["voice"] != "voice-2" {
			t.Fatalf("frame %d voice = %v", i, f["voice"])
		}
		if _, has := f["providerOptions"]; has != (i == 0) {
			t.Fatalf("frame %d providerOptions present = %v", i, has)
		}
		joined.WriteString(f["delta"].(string))
	}
	if joined.String() != long {
		t.Fatal("split frames do not reassemble to the original text")
	}
}

func TestSpeechModel_DoStreamSpeech_EmptyPartsCarryVoiceOrOptions(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text: newSpeechTestText(
			provider.SpeechStreamTextPart{},
			provider.SpeechStreamTextPart{ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}},
			provider.SpeechStreamTextPart{Text: "Hi", Voice: "v2"},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.text-done", 2*time.Second)
	want := []map[string]interface{}{
		{"type": "speech-stream.text-delta", "delta": "", "providerOptions": map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}},
		{"type": "speech-stream.text-delta", "delta": "Hi", "voice": "v2"},
		{"type": "speech-stream.text-done"},
	}
	if got := server.frames()[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("frames = %#v", got)
	}
}

func TestSpeechModel_DoStreamSpeech_TeamSubprotocolFromHeader(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text:    texts("Hi"),
		Headers: map[string]string{"x-vercel-ai-gateway-team": "my-team"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.start", 2*time.Second)
	server.mu.Lock()
	defer server.mu.Unlock()
	if !strings.Contains(server.protocolHeader, "ai-gateway-team.bXktdGVhbQ") {
		t.Fatalf("Sec-WebSocket-Protocol = %q", server.protocolHeader)
	}
}

func TestSpeechModel_DoStreamSpeech_ServerErrorPartBecomesTypedGatewayError(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	text := texts("Hi")
	text.block = true
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.start", 2*time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "Rate limited", "type": "rate_limit_exceeded"}}

	part, err := result.Stream.Next()
	if err != nil || part.Type != "error" {
		t.Fatalf("part = %+v err = %v", part, err)
	}
	var rl *gatewayerrors.GatewayRateLimitError
	if e, ok := part.Err.(error); !ok || !errors.As(e, &rl) {
		t.Fatalf("error part payload = %#v, want a GatewayRateLimitError", part.Err)
	}
	// The error part is terminal: the text is cancelled while the server
	// finishes, then closing without a finish part surfaces the same error.
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text stream was not cancelled after a terminal error part")
	}
	server.closeConnection()
	if _, err := result.Stream.Next(); !errors.As(err, &rl) {
		t.Fatalf("Next() error = %v, want GatewayRateLimitError", err)
	}
}

func TestSpeechModel_DoStreamSpeech_ErrorsWhenClosedBeforeFinish(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: texts("Hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.text-done", 2*time.Second)
	server.closeConnection()
	_, err = result.Stream.Next()
	if err == nil || !strings.HasSuffix(err.Error(), "AI Gateway speech stream closed before a finish part was received.") {
		t.Fatalf("err = %v", err)
	}
}

func TestSpeechModel_DoStreamSpeech_CloseCancelsTextAndSocket(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	text := texts()
	text.block = true
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	server.waitForFrame(t, "speech-stream.start", 2*time.Second)
	_ = result.Stream.Close()
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text stream was not cancelled on Close")
	}
}

func TestSpeechModel_DoStreamSpeech_CancelsTextWhenDialFails(t *testing.T) {
	model := newTestGatewaySpeechModel(t, "http://127.0.0.1:1")
	text := texts("Hi")
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.Stream.Next(); err == nil {
		t.Fatal("expected a connection error")
	}
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text stream was not cancelled after the dial failed")
	}
}

func TestSpeechModel_DoStreamSpeech_BoundsServerFrameSize(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewaySpeechModel(t, server.ts.URL)
	result, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: texts("Hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, "speech-stream.text-done", 2*time.Second)
	server.toSend <- map[string]interface{}{"type": "raw", "rawValue": strings.Repeat("x", maxSpeechServerFrameBytes+1024)}
	_, err = result.Stream.Next()
	if err == nil {
		t.Fatal("an oversized server frame must fail the stream")
	}
}

func TestToGatewaySpeechURL(t *testing.T) {
	if got := ToGatewaySpeechURL("http://localhost:3000/v4/ai", "a/b"); got != "ws://localhost:3000/v4/ai/speech-model?ai-model-id=a%2Fb" {
		t.Fatalf("url = %s", got)
	}
	if got := ToGatewaySpeechURL("https://ai-gateway.vercel.sh/v4/ai", "x"); !strings.HasPrefix(got, "wss://ai-gateway.vercel.sh/v4/ai/speech-model") {
		t.Fatalf("url = %s", got)
	}
}

func TestSplitSpeechTextDelta(t *testing.T) {
	if got := splitSpeechTextDelta("Hello"); !reflect.DeepEqual(got, []string{"Hello"}) {
		t.Fatalf("got %v", got)
	}
	if got := splitSpeechTextDelta(""); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	delta := strings.Repeat("a", 8*1024-1) + "\U0001F44Bb"
	pieces := splitSpeechTextDelta(delta)
	if strings.Join(pieces, "") != delta || !strings.HasSuffix(pieces[0], "a") || !strings.HasPrefix(pieces[1], "\U0001F44B") {
		t.Fatalf("pieces = %q", pieces)
	}
}

// TestGatewayGetSpeechToken mirrors the TS gateway.experimental_speech.getToken
// test: it mints a speech-bound secret (routeKind:"speech") and returns the
// streaming speech WebSocket URL for the model.
func TestGatewayGetSpeechToken(t *testing.T) {
	var body map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_minted","expiresAt":1700000060}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	expires := 120
	result, err := p.GetSpeechToken(context.Background(), "elevenlabs/eleven-flash-v2.5", &SpeechClientSecretOptions{ExpiresAfterSeconds: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if body["routeKind"] != "speech" || body["model"] != "elevenlabs/eleven-flash-v2.5" || body["expiresIn"] != float64(120) {
		t.Fatalf("body = %#v", body)
	}
	wantURL := "ws://" + serverURL[len("http://"):] + "/v4/ai/speech-model?ai-model-id=elevenlabs%2Feleven-flash-v2.5"
	if result.Token != "vcst_minted" || result.URL != wantURL || result.ExpiresAt == nil || *result.ExpiresAt != 1700000060 {
		t.Fatalf("result = %#v, want URL %q", result, wantURL)
	}
	var _ provider.SpeechStreamer = p.ExperimentalSpeech("m")
}
