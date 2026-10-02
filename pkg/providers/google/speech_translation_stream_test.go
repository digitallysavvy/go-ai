package google

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// newSpeechTranslationTestModel builds a SpeechTranslationModel pointed at a
// test server, with finishGrace overridden for fast, deterministic tests
// (mirrors TS createModel's `_internal.finishGrace` override). grace <= 0
// leaves the field unset (production default of
// speechTranslationDefaultFinishGraceDuration applies).
func newSpeechTranslationTestModel(t *testing.T, baseURL string, grace time.Duration) *SpeechTranslationModel {
	t.Helper()
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	m := NewSpeechTranslationModel(p, "gemini-3.5-live-translate-preview")
	m.finishGrace = grace
	return m
}

func drainUntilTranslationFinishOrError(t *testing.T, stream provider.SpeechTranslationStream) ([]*provider.SpeechTranslationStreamPart, error) {
	t.Helper()
	var parts []*provider.SpeechTranslationStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, part)
		if part.Type == provider.SpeechTranslationStreamPartTypeFinish {
			return parts, nil
		}
	}
}

// TestSpeechTranslationModel_BaseHeadersCarryUserAgentTag mirrors TS
// google-live-speech-translation-model.ts's doStream, which reuses
// `this.config.headers()` -- the same tagged getHeaders() closure used for
// REST calls -- for the WebSocket handshake, so it carries the
// `ai-sdk-google/VERSION` tag too.
func TestSpeechTranslationModel_BaseHeadersCarryUserAgentTag(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	m := NewSpeechTranslationModel(p, "gemini-3.5-live-translate-preview")

	ua := m.baseSpeechTranslationHeaders()["user-agent"]
	if !strings.HasPrefix(ua, "ai-sdk-google/") {
		t.Fatalf("user-agent = %q, want ai-sdk-google/... prefix", ua)
	}
}

func TestSpeechTranslationModel_DoStream_RequiresTargetLanguage(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	m := NewSpeechTranslationModel(p, "gemini-3.5-live-translate-preview")
	_, err := m.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) || !strings.Contains(err.Error(), "targetLanguage is required") {
		t.Fatalf("err = %v, want an InvalidArgumentError about targetLanguage", err)
	}
}

func TestSpeechTranslationModel_DoStream_RejectsNonPCMOrWrongRate(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	m := NewSpeechTranslationModel(p, "gemini-3.5-live-translate-preview")

	_, err := m.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcmu", Rate: intPtr(8000)},
		TargetLanguage:   "es",
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 16kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 16kHz PCM message", err)
	}

	_, err = m.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(24000)},
		TargetLanguage:   "es",
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 16kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 16kHz PCM message for a non-16kHz rate", err)
	}
}

func TestSpeechTranslationModel_DoStream_RequiresAPIKey(t *testing.T) {
	p := New(Config{})
	m := NewSpeechTranslationModel(p, "gemini-3.5-live-translate-preview")
	_, err := m.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err == nil || !strings.Contains(err.Error(), "API key is required for streaming translation") {
		t.Fatalf("err = %v, want a missing-API-key message", err)
	}
}

func TestSpeechTranslationModel_DoStream_URLIncludesAPIKeyAndModelPath(t *testing.T) {
	got := getLiveSpeechTranslationWebSocketURL("https://generativelanguage.googleapis.com/v1beta", "test-api-key")
	want := "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=test-api-key"
	if got != want {
		t.Fatalf("getLiveSpeechTranslationWebSocketURL() = %q, want %q", got, want)
	}
}

// TestSpeechTranslationModel_DoStream_StreamsTurnEndToEnd mirrors the TS
// "should stream translation over the Gemini Live API WebSocket" test.
func TestSpeechTranslationModel_DoStream_StreamsTurnEndToEnd(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	audio := newChanAudioStream([]byte{1, 2, 3})

	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"echoTargetLanguage": true},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	setupMsg := server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	setup, _ := setupMsg["setup"].(map[string]interface{})
	if setup["model"] != "models/gemini-3.5-live-translate-preview" {
		t.Fatalf("setup.model = %v", setup["model"])
	}
	generationConfig, _ := setup["generationConfig"].(map[string]interface{})
	translationConfig, _ := generationConfig["translationConfig"].(map[string]interface{})
	if translationConfig["targetLanguageCode"] != "es" || translationConfig["echoTargetLanguage"] != true {
		t.Fatalf("translationConfig = %+v", translationConfig)
	}

	// audio is gated on setupComplete: nothing else should have been sent yet.
	if got := server.all(); len(got) != 1 {
		t.Fatalf("received before setupComplete = %+v, want only the setup message", got)
	}

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	if first.Type != provider.SpeechTranslationStreamPartTypeStreamStart || len(first.Warnings) != 0 {
		t.Fatalf("first part = %+v", first)
	}

	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}

	appendMsg := server.waitFor(t, hasRealtimeInputKey("audio"), time.Second)
	realtimeInput := appendMsg["realtimeInput"].(map[string]interface{})
	audioPart := realtimeInput["audio"].(map[string]interface{})
	if mimeType, _ := audioPart["mimeType"].(string); mimeType != "audio/pcm;rate=16000" {
		t.Fatalf("mimeType = %q", mimeType)
	}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"inputTranscription": map[string]interface{}{"text": "Hello"}}}
	server.toSend <- map[string]interface{}{
		"serverContent": map[string]interface{}{
			"modelTurn":           map[string]interface{}{"parts": []interface{}{map[string]interface{}{"inlineData": map[string]interface{}{"data": "BAUG"}}}},
			"outputTranscription": map[string]interface{}{"text": "Hola"},
		},
	}
	server.toSend <- map[string]interface{}{
		"usageMetadata": map[string]interface{}{
			"promptTokensDetails":   []interface{}{map[string]interface{}{"modality": "AUDIO", "tokenCount": 10}, map[string]interface{}{"modality": "TEXT", "tokenCount": 2}},
			"responseTokensDetails": []interface{}{map[string]interface{}{"modality": "AUDIO", "tokenCount": 20}},
		},
	}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}

	rest, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	parts := append([]*provider.SpeechTranslationStreamPart{first}, rest...)
	// stream-start, source-delta, audio, output-delta, source-final,
	// output-final, finish.
	if len(parts) != 7 {
		t.Fatalf("len(parts) = %d, want 7; got %+v", len(parts), parts)
	}
	if parts[1].Type != provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta || parts[1].Delta != "Hello" {
		t.Fatalf("parts[1] = %+v", parts[1])
	}
	wantAudio, _ := base64.StdEncoding.DecodeString("BAUG")
	if parts[2].Type != provider.SpeechTranslationStreamPartTypeAudio || string(parts[2].AudioData) != string(wantAudio) {
		t.Fatalf("parts[2] = %+v", parts[2])
	}
	if parts[3].Type != provider.SpeechTranslationStreamPartTypeOutputTextDelta || parts[3].Delta != "Hola" {
		t.Fatalf("parts[3] = %+v", parts[3])
	}
	if parts[4].Type != provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal || parts[4].Text != "Hello" {
		t.Fatalf("parts[4] = %+v", parts[4])
	}
	if parts[5].Type != provider.SpeechTranslationStreamPartTypeOutputTextFinal || parts[5].Text != "Hola" {
		t.Fatalf("parts[5] = %+v", parts[5])
	}
	finish := parts[6]
	if finish.Type != provider.SpeechTranslationStreamPartTypeFinish || finish.SourceText != "Hello" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
	if finish.Usage == nil || finish.Usage.InputAudioTokens == nil || *finish.Usage.InputAudioTokens != 10 ||
		finish.Usage.OutputAudioTokens == nil || *finish.Usage.OutputAudioTokens != 20 {
		t.Fatalf("finish.Usage = %+v", finish.Usage)
	}
}

// TestSpeechTranslationModel_DoStream_WarnsUnsupportedSourceLanguageAndOutputFormat
// mirrors the TS "should warn about unsupported sourceLanguage and
// outputAudioFormat" test.
func TestSpeechTranslationModel_DoStream_WarnsUnsupportedSourceLanguageAndOutputFormat(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:             newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat:  provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:    "es",
		SourceLanguage:    "en",
		OutputAudioFormat: &provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(24000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	if len(first.Warnings) != 2 {
		t.Fatalf("warnings = %+v, want 2", first.Warnings)
	}
	if first.Warnings[0].Feature != "sourceLanguage" || first.Warnings[1].Feature != "outputAudioFormat" {
		t.Fatalf("warnings = %+v", first.Warnings)
	}
}

// TestSpeechTranslationModel_DoStream_AccumulatesMultipleTurnsBeforeAudioEnds
// mirrors the TS "should accumulate multiple turns before the audio ends" test.
func TestSpeechTranslationModel_DoStream_AccumulatesMultipleTurnsBeforeAudioEnds(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
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
	// newBlockingAudioStream's Next() never returns on its own; feed one
	// chunk directly so the pump has something to send, keeping the stream
	// "open" (not yet at EOF) for the first turn.
	audio.chunks <- []byte{1, 2, 3}
	server.waitFor(t, hasRealtimeInputKey("audio"), time.Second)

	// First turn completes while audio is still streaming: no finish yet.
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"inputTranscription":  map[string]interface{}{"text": "Hello "},
		"outputTranscription": map[string]interface{}{"text": "Hola "},
	}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}

	// give the (unbounded) stream time to process before closing audio.
	time.Sleep(20 * time.Millisecond)

	// audio ends; second turn completes and finishes the stream.
	audio.mu.Lock()
	close(audio.chunks)
	audio.mu.Unlock()
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"inputTranscription":  map[string]interface{}{"text": "world"},
		"outputTranscription": map[string]interface{}{"text": "mundo"},
	}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "Hello world" || finish.OutputText != "Hola mundo" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_FinishesWithoutFurtherMessages mirrors
// TS "should finish without further server messages when the turn completed
// before the audio ended": the finish-grace timer alone must terminate the
// stream once audio has ended and a prior turnComplete was already seen.
func TestSpeechTranslationModel_DoStream_FinishesWithoutFurtherMessages(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	// Small but not racy: real timer must fire, but well under the
	// waitFor/drain timeouts.
	model := newSpeechTranslationTestModel(t, server.ts.URL, 30*time.Millisecond)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
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
	audio.chunks <- []byte{1, 2, 3}
	server.waitFor(t, hasRealtimeInputKey("audio"), time.Second)

	// the turn completes while the input audio stream is still open
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"inputTranscription":  map[string]interface{}{"text": "Hello"},
		"outputTranscription": map[string]interface{}{"text": "Hola"},
	}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}
	time.Sleep(20 * time.Millisecond)

	// the audio then ends; the already-received turnComplete satisfies the
	// finish condition without any further server message.
	audio.mu.Lock()
	close(audio.chunks)
	audio.mu.Unlock()

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "Hello" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
}

// pcm16Silence returns count silent (zero-amplitude) little-endian PCM16
// samples as raw bytes, base64-encoded, matching TS's
// convertUint8ArrayToBase64(new Uint8Array(byteLength)).
func pcm16Silence(sampleCount int) string {
	buf := make([]byte, sampleCount*2)
	for i := 0; i < sampleCount; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], 0)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// TestSpeechTranslationModel_DoStream_TrailingSilenceFinish mirrors TS
// "should finish continuous translation after trailing output silence": the
// silence threshold is computed from decoded PCM16 sample counts, not real
// elapsed time, so this test needs no wall-clock waiting.
func TestSpeechTranslationModel_DoStream_TrailingSilenceFinish(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 500*time.Millisecond)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
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

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"inputTranscription":  map[string]interface{}{"text": "Hello"},
		"outputTranscription": map[string]interface{}{"text": "Hola"},
	}}
	// a tiny non-silent chunk first (matches TS): resets any trailing-silence
	// accumulation before the real silence begins.
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"modelTurn": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"inlineData": map[string]interface{}{"data": base64.StdEncoding.EncodeToString([]byte{1, 0})}}}},
	}}

	// 6000 samples at 24kHz = 250ms; two chunks reach the 500ms threshold.
	silence := pcm16Silence(6000)
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"modelTurn": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"inlineData": map[string]interface{}{"data": silence}}}},
	}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{
		"modelTurn": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"inlineData": map[string]interface{}{"data": silence}}}},
	}}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "Hello" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_FinishWhenCloseWhilePending mirrors TS
// "should finish when the server closes while a finish is pending": a large
// grace period so the pending finish is still scheduled (not fired) when the
// server closes the connection.
func TestSpeechTranslationModel_DoStream_FinishWhenCloseWhilePending(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 10*time.Second)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
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

	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"outputTranscription": map[string]interface{}{"text": "Hola"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}
	time.Sleep(20 * time.Millisecond)

	// close while the (10s) grace-period finish is pending confirms completion:
	server.closeConnection()

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_ErrorsOnCloseBeforeFinishing mirrors TS
// "should error the stream with close diagnostics when the socket closes
// before finishing": no turnComplete/finish-grace timer was ever scheduled,
// so the close is an unexpected termination.
func TestSpeechTranslationModel_DoStream_ErrorsOnCloseBeforeFinishing(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"outputTranscription": map[string]interface{}{"text": "Ho"}}}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	_, err = drainUntilTranslationFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "closed unexpectedly before finishing") {
		t.Fatalf("err = %v, want a close-diagnostics message", err)
	}
}

// TestSpeechTranslationModel_DoStream_ErrorsOnServerErrorMessage mirrors TS
// "should error the stream on server error messages".
func TestSpeechTranslationModel_DoStream_ErrorsOnServerErrorMessage(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	audio := newChanAudioStream([]byte{1})
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"error": map[string]interface{}{"message": "invalid target language"}}

	_, err = drainUntilTranslationFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "invalid target language") {
		t.Fatalf("err = %v, want containing 'invalid target language'", err)
	}
}

// TestSpeechTranslationModel_DoStream_IncludeRawChunks mirrors TS "should
// include raw provider chunks when includeRawChunks is enabled".
func TestSpeechTranslationModel_DoStream_IncludeRawChunks(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	type drainResult struct {
		parts []*provider.SpeechTranslationStreamPart
		err   error
	}
	drained := make(chan drainResult, 1)
	go func() {
		parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
		drained <- drainResult{parts: parts, err: err}
	}()

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), time.Second)
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"outputTranscription": map[string]interface{}{"text": "Hola"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}

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
		if p.Type == provider.SpeechTranslationStreamPartTypeRaw {
			rawCount++
		}
	}
	if rawCount == 0 {
		t.Fatal("expected at least one raw chunk")
	}
}

// TestSpeechTranslationModel_DoStream_IgnoresMessagesAfterFinish mirrors TS
// "should ignore server messages after the stream finished".
func TestSpeechTranslationModel_DoStream_IgnoresMessagesAfterFinish(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"outputTranscription": map[string]interface{}{"text": "Hola"}}}
	server.toSend <- map[string]interface{}{"serverContent": map[string]interface{}{"turnComplete": true}}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if parts[len(parts)-1].Type != provider.SpeechTranslationStreamPartTypeFinish {
		t.Fatalf("last part = %+v, want finish", parts[len(parts)-1])
	}

	// post-finish messages are a no-op: run() has already returned, so
	// Next() simply reports the terminal state again (io.EOF, no error).
	if _, err := result.Stream.Next(); err == nil {
		t.Fatal("expected io.EOF after the stream finished")
	}
}

func TestSpeechTranslationModel_DoStream_CancelMidStream(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()

	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	audio := newBlockingAudioStream()
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
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
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cancelled, _ := audio.wasCancelled(); cancelled {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the AudioStream to be cancelled after Close()")
}

// TestSpeechTranslationModel_DoStream_AbnormalDisconnectSurfacesError mirrors
// TS connectToWebSocket's onSocketError semantics for speech translation (see
// the matching transcription test for the full rationale).
func TestSpeechTranslationModel_DoStream_AbnormalDisconnectSurfacesError(t *testing.T) {
	ts := newGoogleAbnormalDisconnectTestServer(t)
	defer ts.Close()

	model := newSpeechTranslationTestModel(t, ts.URL, 0)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	_, err = drainUntilTranslationFinishOrError(t, result.Stream)
	if err == nil {
		t.Fatal("expected an error for an abnormal disconnection, got a silent finish")
	}
}

// TestProvider_Translation_CreatesSpeechTranslationModel mirrors TS "should
// be created by the provider translation factory".
func TestProvider_Translation_CreatesSpeechTranslationModel(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	m, err := p.Translation("gemini-3.5-live-translate-preview")
	if err != nil {
		t.Fatalf("Translation() error = %v", err)
	}
	if m.Provider() != "google.generative-ai.speech-translation" {
		t.Fatalf("Provider() = %q", m.Provider())
	}
	if m.ModelID() != "gemini-3.5-live-translate-preview" {
		t.Fatalf("ModelID() = %q", m.ModelID())
	}
}
