package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// speechText is a provider.SpeechTextStream test double over a fixed list of
// parts. With block set it waits (honoring ctx) instead of ending.
type speechText struct {
	mu        sync.Mutex
	parts     []provider.SpeechStreamTextPart
	i         int
	block     bool
	release   chan struct{} // closed to end a blocking stream
	cancelled chan struct{}
	once      sync.Once
}

func newSpeechText(parts ...provider.SpeechStreamTextPart) *speechText {
	return &speechText{parts: parts, cancelled: make(chan struct{}), release: make(chan struct{})}
}

func speechTexts(ss ...string) *speechText {
	parts := make([]provider.SpeechStreamTextPart, len(ss))
	for i, s := range ss {
		parts[i] = provider.SpeechStreamTextPart{Text: s}
	}
	return newSpeechText(parts...)
}

func (s *speechText) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.release:
		}
	}
	return nil, io.EOF
}

func (s *speechText) Cancel(error) { s.once.Do(func() { close(s.cancelled) }) }

func (s *speechText) wasCancelled() bool {
	select {
	case <-s.cancelled:
		return true
	default:
		return false
	}
}

func newSpeechTestModel(t *testing.T, baseURL, modelID string) *SpeechModel {
	t.Helper()
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	return NewSpeechModel(p, modelID)
}

// startSpeech starts a stream against a fresh test server.
func startSpeech(t *testing.T, modelID string, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, *realtimeTestServer) {
	t.Helper()
	server := newRealtimeTestServer(t)
	t.Cleanup(server.close)
	result, err := newSpeechTestModel(t, server.ts.URL, modelID).DoStreamSpeech(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStreamSpeech() error = %v", err)
	}
	t.Cleanup(func() { _ = result.Stream.Close() })
	return result, server
}

func collectSpeech(t *testing.T, stream provider.SpeechStream) ([]provider.SpeechStreamPart, error) {
	t.Helper()
	var parts []provider.SpeechStreamPart
	for {
		part, err := stream.Next()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return parts, err
		}
		parts = append(parts, *part)
	}
}

func m(kv ...interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func TestSpeechStream_StreamsTextDeltasAndAudio(t *testing.T) {
	speed := 1.1
	r24 := 24000
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text:         speechTexts("Hello ", "", "wörld 👋 "),
		Voice:        "voice-1",
		OutputFormat: &provider.AudioFormat{Type: "audio/pcm", Rate: &r24},
		Speed:        &speed,
		Language:     "en",
		ProviderOptions: m("elevenlabs", m(
			"voiceSettings", m("stability", 0.4, "similarityBoost", 0.8),
			"seed", 7.0, "applyTextNormalization", "off",
			"streaming", m("autoMode", true, "chunkLengthSchedule", []interface{}{50.0, 120.0}, "inactivityTimeout", 60.0, "syncAlignment", true),
		)),
	})

	body := result.RequestBody.(map[string]interface{})
	wantURL := "ws://" + strings.TrimPrefix(server.ts.URL, "http://") + "/v1/text-to-speech/voice-1/stream-input?model_id=eleven_flash_v2_5&output_format=pcm_24000&language_code=en&inactivity_timeout=60&sync_alignment=true&auto_mode=true&apply_text_normalization=off&seed=7"
	if body["url"] != wantURL {
		t.Fatalf("url = %v\nwant %v", body["url"], wantURL)
	}
	if strings.Contains(strings.ToLower(toJSON(t, result.RequestBody)), "test-api-key") {
		t.Fatal("the API key must stay out of the reported request body")
	}

	got := server.waitForCount(t, 4, 2*time.Second)
	want := []map[string]interface{}{
		m("text", " ", "voice_settings", m("stability", 0.4, "similarity_boost", 0.8, "speed", 1.1),
			"generation_config", m("chunk_length_schedule", []interface{}{50.0, 120.0}), "xi-api-key", "test-api-key"),
		m("text", "Hello "), m("text", "wörld 👋 "), m("text", ""),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v\nwant %#v", got, want)
	}
	// Authenticated in the init message, not the handshake.
	server.mu.Lock()
	apiKeyHeader := server.handshakeAPIKey
	server.mu.Unlock()
	if apiKeyHeader != "" {
		t.Fatalf("handshake carried the API key header: %q", apiKeyHeader)
	}

	server.toSend <- m("audio", "AQI=", "alignment", m("chars", []interface{}{"H"}, "charStartTimesMs", []interface{}{0}, "charDurationsMs", []interface{}{5}))
	server.toSend <- m("audio", "AwQ=")
	server.toSend <- m("audio", nil, "isFinal", true)

	parts, err := collectSpeech(t, result.Stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 4 || parts[0].Type != "stream-start" || len(parts[0].Warnings) != 0 {
		t.Fatalf("parts = %+v", parts)
	}
	a1, a2, fin := parts[1], parts[2], parts[3]
	if !reflect.DeepEqual(a1.Audio, []byte{1, 2}) || a1.AudioFormat.Type != "audio/pcm" || *a1.AudioFormat.Rate != 24000 {
		t.Fatalf("a1 = %+v", a1)
	}
	if !reflect.DeepEqual(a1.ProviderMetadata, m("elevenlabs", m("alignment", m("chars", []interface{}{"H"}, "charStartTimesMs", []interface{}{0.0}, "charDurationsMs", []interface{}{5.0})))) {
		t.Fatalf("a1 metadata = %#v", a1.ProviderMetadata)
	}
	if !reflect.DeepEqual(a2.Audio, []byte{3, 4}) || a2.ProviderMetadata != nil {
		t.Fatalf("a2 = %+v", a2)
	}
	// 'Hello ' (6) + 'wörld 👋 ' (8 code points, 9 UTF-16 units)
	if fin.Type != "finish" || *fin.Usage.InputCharacters != 14 {
		t.Fatalf("finish = %+v", fin)
	}
}

func toJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSpeechStream_HoldsPartialWordsUntilBoundary(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text: speechTexts("Hel", "lo wor", "ld, how a", "re", " you"),
	})
	got := server.waitForCount(t, 6, 2*time.Second)[1:]
	want := []map[string]interface{}{m("text", "Hello "), m("text", "world, how "), m("text", "are "), m("text", "you"), m("text", "")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
	server.toSend <- m("audio", "AQI=")
	server.toSend <- m("isFinal", true)
	parts, err := collectSpeech(t, result.Stream)
	if err != nil {
		t.Fatal(err)
	}
	if last := parts[len(parts)-1]; *last.Usage.InputCharacters != 24 {
		t.Fatalf("usage = %+v", last.Usage)
	}
}

func TestSpeechStream_DoesNotHoldTextWithoutSpacesIndefinitely(t *testing.T) {
	_, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text: speechTexts("こんにちは", "世界。次", strings.Repeat("x", 100)),
	})
	got := server.waitForCount(t, 4, 2*time.Second)[1:]
	want := []map[string]interface{}{m("text", "こんにちは世界。"), m("text", "次"+strings.Repeat("x", 100)), m("text", "")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestTakeCompleteWords_FollowsJSWhitespace(t *testing.T) {
	cases := []struct{ in, send, hold string }{
		{"abc", "", "abc"},
		{"ab cd", "ab ", "cd"},
		{"ab cd", "ab ", "cd"},           // NBSP is \s in JS
		{"ab\ufeffcd", "ab\ufeff", "cd"}, // BOM is \s in JS, not in Go
		{"ab\u0085cd", "", "ab\u0085cd"}, // NEL is not \s in JS
		{"你好，世界", "你好，", "世界"},
	}
	for _, c := range cases {
		if send, hold := takeCompleteWords(c.in); send != c.send || hold != c.hold {
			t.Errorf("takeCompleteWords(%q) = %q, %q; want %q, %q", c.in, send, hold, c.send, c.hold)
		}
	}
}

func withKeepalive(t *testing.T, d time.Duration) {
	t.Helper()
	old := speechKeepaliveInterval
	speechKeepaliveInterval = d
	t.Cleanup(func() { speechKeepaliveInterval = old })
}

func TestSpeechStream_KeepalivesWhileIdleAndNoneAfterInputEnds(t *testing.T) {
	withKeepalive(t, 40*time.Millisecond)
	text := newSpeechText(provider.SpeechStreamTextPart{Text: "Thinking... "})
	text.block = true
	_, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: text})

	got := server.waitForCount(t, 3, 2*time.Second)[1:]
	if !reflect.DeepEqual(got[:2], []map[string]interface{}{m("text", "Thinking... "), m("text", " ")}) {
		t.Fatalf("messages = %#v", got)
	}
	close(text.release)
	// A keepalive may race ahead of the end message, so wait for it by value.
	deadline := time.Now().Add(2 * time.Second)
	for {
		found := false
		for _, msg := range server.all()[1:] {
			if reflect.DeepEqual(msg, m("text", "")) {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("end-of-input message not sent")
		}
		time.Sleep(5 * time.Millisecond)
	}
	end := len(server.all())
	time.Sleep(200 * time.Millisecond)
	if after := len(server.all()); after != end {
		t.Fatalf("keepalives continued after the input ended: %d -> %d", end, after)
	}
}

func TestSpeechStream_NoKeepalivesWhenInactivityTimeoutIsSet(t *testing.T) {
	withKeepalive(t, 30*time.Millisecond)
	text := newSpeechText(provider.SpeechStreamTextPart{Text: "Thinking... "})
	text.block = true
	_, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text:            text,
		ProviderOptions: m("elevenlabs", m("streaming", m("inactivityTimeout", 20.0))),
	})
	server.waitForCount(t, 2, 2*time.Second)
	time.Sleep(200 * time.Millisecond)
	if got := server.all()[1:]; !reflect.DeepEqual(got, []map[string]interface{}{m("text", "Thinking... ")}) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSpeechStream_ErrorsWhenAPartRequestsAnotherVoice(t *testing.T) {
	result, _ := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Voice: "voice-1",
		Text: newSpeechText(
			provider.SpeechStreamTextPart{Text: "Hello ", Voice: "voice-1"},
			provider.SpeechStreamTextPart{Text: "there ", Voice: "voice-2"},
		),
	})
	_, err := collectSpeech(t, result.Stream)
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestSpeechStream_DefaultsVoiceAndOutputFormat(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi")})
	want := "ws://" + strings.TrimPrefix(server.ts.URL, "http://") + "/v1/text-to-speech/21m00Tcm4TlvDq8ikWAM/stream-input?model_id=eleven_flash_v2_5&output_format=mp3_44100_128"
	if got := result.RequestBody.(map[string]interface{})["url"]; got != want {
		t.Fatalf("url = %v", got)
	}
}

func TestSpeechStream_IncludesRawChunks(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi"), IncludeRawChunks: true})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- m("audio", "AQI=")
	server.toSend <- m("isFinal", true)
	parts, err := collectSpeech(t, result.Stream)
	if err != nil {
		t.Fatal(err)
	}
	var raws []interface{}
	for _, p := range parts {
		if p.Type == "raw" {
			raws = append(raws, p.RawValue)
		}
	}
	if !reflect.DeepEqual(raws, []interface{}{m("audio", "AQI="), m("isFinal", true)}) {
		t.Fatalf("raw = %#v", raws)
	}
}

func TestSpeechStream_WarnsAboutUnsupportedOptions(t *testing.T) {
	result, _ := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text: speechTexts("Hi"), Instructions: "whisper",
		ProviderOptions: m("elevenlabs", m("previousText", "Before.")),
	})
	part, err := result.Stream.Next()
	if err != nil || part.Type != "stream-start" {
		t.Fatalf("part = %+v err = %v", part, err)
	}
	if len(part.Warnings) != 2 ||
		part.Warnings[0].Feature != "instructions" || part.Warnings[0].Details != "ElevenLabs speech models do not support instructions. Instructions parameter was ignored." ||
		part.Warnings[1].Feature != "providerOptions.elevenlabs.previousText" || part.Warnings[1].Details != "ElevenLabs streaming speech does not support previousText." {
		t.Fatalf("warnings = %+v", part.Warnings)
	}
}

func TestSpeechStream_ServerErrorMessageFailsTheStream(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi")})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- m("message", "Invalid API key", "error", "auth_error")
	_, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != "ElevenLabs speech stream error: Invalid API key" {
		t.Fatalf("err = %v", err)
	}
}

func TestSpeechStream_ErrorsWhenSocketClosesBeforeIsFinal(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi")})
	server.waitForCount(t, 3, 2*time.Second)
	server.closeConnection()
	_, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != "ElevenLabs speech stream closed before completion (code 1000)." {
		t.Fatalf("err = %v", err)
	}
}

func TestSpeechStream_CloseCancelsTextAndSocket(t *testing.T) {
	text := speechTexts()
	text.block = true
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: text})
	server.waitForCount(t, 1, 2*time.Second)
	_ = result.Stream.Close()
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text was not cancelled when the stream was closed")
	}
}

func TestSpeechStream_ContextCancelStopsTheStream(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	text := speechTexts()
	text.block = true
	ctx, cancel := context.WithCancel(context.Background())
	result, err := newSpeechTestModel(t, server.ts.URL, "eleven_flash_v2_5").DoStreamSpeech(ctx, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForCount(t, 1, 2*time.Second)
	cancel()
	// stream-start was emitted first; the failure follows.
	_, err = collectSpeech(t, result.Stream)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if !text.wasCancelled() {
		t.Fatal("text was not cancelled")
	}
}

func TestSpeechStream_CancelsTextWhenDialFails(t *testing.T) {
	text := speechTexts("Hi")
	result, err := newSpeechTestModel(t, "http://127.0.0.1:1", "eleven_flash_v2_5").DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collectSpeech(t, result.Stream); err == nil {
		t.Fatal("expected a connection error")
	}
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text was not cancelled after the dial failed")
	}
}

func TestSpeechStream_SendsVersionedPronunciationLocators(t *testing.T) {
	_, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text: speechTexts("Hi"),
		ProviderOptions: m("elevenlabs", m("pronunciationDictionaryLocators", []PronunciationDictionaryLocator{
			{PronunciationDictionaryID: "dict", VersionID: "v1"},
		})),
	})
	first := server.waitForCount(t, 1, 2*time.Second)[0]
	want := []interface{}{m("pronunciation_dictionary_id", "dict", "version_id", "v1")}
	if !reflect.DeepEqual(first["pronunciation_dictionary_locators"], want) {
		t.Fatalf("locators = %#v", first["pronunciation_dictionary_locators"])
	}
}

func TestSpeechStream_SetupErrorsFailBeforeConnectingAndCancelText(t *testing.T) {
	cases := map[string]struct {
		model string
		opts  map[string]interface{}
		want  interface{}
	}{
		"unversioned locator":            {"eleven_flash_v2_5", m("elevenlabs", m("pronunciationDictionaryLocators", []PronunciationDictionaryLocator{{PronunciationDictionaryID: "dict"}})), &providererrors.InvalidArgumentError{}},
		"v3 without dialogue":            {"eleven_v3", nil, &providererrors.UnsupportedFunctionalityError{}},
		"v3 conversational":              {"eleven_v3_conversational", nil, &providererrors.UnsupportedFunctionalityError{}},
		"v4 without dialogue":            {"eleven_v4", nil, &providererrors.UnsupportedFunctionalityError{}},
		"dialogue on non-dialogue model": {"eleven_flash_v2_5", m("elevenlabs", m("dialogue", m("voices", []interface{}{"a"}))), &providererrors.UnsupportedFunctionalityError{}},
		"call voice not in dialogue":     {"eleven_v4", m("elevenlabs", m("dialogue", m("voices", []interface{}{"a"}))), &providererrors.InvalidArgumentError{}},
		"conversational needs one voice": {"eleven_v3_conversational", m("elevenlabs", m("dialogue", m("voices", []interface{}{"a", "b"}))), &providererrors.InvalidArgumentError{}},
		"unsupported output format":      {"eleven_flash_v2_5", nil, &providererrors.InvalidArgumentError{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			server := newRealtimeTestServer(t)
			defer server.close()
			text := speechTexts("Hi")
			opts := &provider.SpeechStreamOptions{Text: text, ProviderOptions: c.opts}
			if name == "call voice not in dialogue" {
				opts.Voice = "c"
			}
			if name == "unsupported output format" {
				opts.OutputFormat = &provider.AudioFormat{Type: "audio/wav"}
			}
			_, err := newSpeechTestModel(t, server.ts.URL, c.model).DoStreamSpeech(context.Background(), opts)
			if err == nil || reflect.TypeOf(err) != reflect.TypeOf(c.want) {
				t.Fatalf("err = %v (%T), want %T", err, err, c.want)
			}
			if !text.wasCancelled() {
				t.Fatal("text stream must be cancelled when setup fails")
			}
			if n := len(server.all()); n != 0 {
				t.Fatalf("setup failure must not connect; server saw %d messages", n)
			}
		})
	}
}

func TestSpeechStream_DefaultBaseURLIsResolved(t *testing.T) {
	if got := New(Config{APIKey: "k"}).config.BaseURL; got != "https://api.elevenlabs.io" {
		t.Fatalf("default BaseURL = %q", got)
	}
	u, err := toWebSocketURL(New(Config{APIKey: "k"}).config.BaseURL, "/v1/text-to-speech/v/stream-input")
	if err != nil || u.String() != "wss://api.elevenlabs.io/v1/text-to-speech/v/stream-input" {
		t.Fatalf("url = %v err = %v", u, err)
	}
}

func TestSpeechStream_VoiceIsPathEscapedAndCredentialsStayOnConfiguredHost(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{
		Text: speechTexts("Hi"), Voice: "../evil@host/x?y#z",
	})
	got := result.RequestBody.(map[string]interface{})["url"].(string)
	if !strings.HasPrefix(got, "ws://"+strings.TrimPrefix(server.ts.URL, "http://")+"/v1/text-to-speech/..%2Fevil%40host%2Fx%3Fy%23z/stream-input?") {
		t.Fatalf("url = %s", got)
	}
}

func TestSpeechStream_BoundsServerFrameSize(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi"), IncludeRawChunks: true})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- m("audio", strings.Repeat("A", maxSpeechServerFrameBytes+1024))
	if _, err := collectSpeech(t, result.Stream); err == nil {
		t.Fatal("an oversized server frame must fail the stream")
	}
}

func TestCountCodePoints(t *testing.T) {
	if got := countCodePoints("a👋é"); got != 3 {
		t.Fatalf("got %d", got)
	}
}

func TestGetElevenLabsAudioFormat(t *testing.T) {
	r := func(v int) *int { return &v }
	for in, want := range map[string]provider.AudioFormat{
		"mp3_44100_128": {Type: "audio/mpeg", Rate: r(44100)},
		"pcm_16000":     {Type: "audio/pcm", Rate: r(16000)},
		"ulaw_8000":     {Type: "audio/pcmu", Rate: r(8000)},
		"alaw_8000":     {Type: "audio/pcma", Rate: r(8000)},
		"opus_48000_64": {Type: "audio/ogg", Rate: r(48000)},
		"unknown":       {Type: "application/octet-stream"},
	} {
		if got := getElevenLabsAudioFormat(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v want %+v", in, got, want)
		}
	}
}

func TestToElevenLabsStreamOutputFormat(t *testing.T) {
	r := func(v int) *int { return &v }
	ok := []struct {
		in   *provider.AudioFormat
		want string
	}{
		{nil, "mp3_44100_128"},
		{&provider.AudioFormat{Type: "audio/mpeg"}, "mp3_44100_128"},
		{&provider.AudioFormat{Type: "audio/mpeg", Rate: r(22050)}, "mp3_22050_32"},
		{&provider.AudioFormat{Type: "audio/pcm"}, "pcm_24000"},
		{&provider.AudioFormat{Type: "audio/pcm", Rate: r(16000)}, "pcm_16000"},
		{&provider.AudioFormat{Type: "audio/pcmu"}, "ulaw_8000"},
		{&provider.AudioFormat{Type: "audio/pcma", Rate: r(8000)}, "alaw_8000"},
		{&provider.AudioFormat{Type: "audio/opus"}, "opus_48000_64"},
		{&provider.AudioFormat{Type: "Audio/L16; rate=16000", Rate: r(16000)}, "pcm_16000"},
	}
	for _, c := range ok {
		if got, err := toElevenLabsStreamOutputFormat(c.in); err != nil || got != c.want {
			t.Errorf("%+v: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []*provider.AudioFormat{
		{Type: "audio/pcm", Rate: r(11025)}, {Type: "audio/wav"}, {Type: "audio/mpeg", Rate: r(48000)},
	} {
		_, err := toElevenLabsStreamOutputFormat(bad)
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Errorf("%+v: err = %v, want InvalidArgumentError", bad, err)
		}
	}
}

// GenerateSpeech ignores the streaming-only options with warnings.
func TestSpeechGenerate_WarnsAboutStreamingAndDialogueOptions(t *testing.T) {
	model := NewSpeechModel(New(Config{APIKey: "k"}), "eleven_v4")
	_, _, warnings, _ := model.buildRequestArgs(&provider.SpeechGenerateOptions{
		Text: "Hi",
		ProviderOptions: m("elevenlabs", m(
			"streaming", m("syncAlignment", true),
			"dialogue", m("voices", []interface{}{"a"}),
		)),
	})
	if len(warnings) != 2 ||
		warnings[0].Feature != "providerOptions.elevenlabs.streaming" ||
		warnings[0].Details != "Streaming options apply only to streaming speech (ExperimentalStreamSpeech) and were ignored." ||
		warnings[1].Feature != "providerOptions.elevenlabs.dialogue" ||
		warnings[1].Details != "Dialogue options apply only to streaming speech (ExperimentalStreamSpeech) and were ignored." {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TS reports a non-string `error` as JSON.stringify(raw.error): wire key
// order and no HTML escaping.
func TestSpeechStream_ObjectErrorTextKeepsWireOrder(t *testing.T) {
	text := `{"error":{"type":"quota","detail":"a<b>&c","code":7}}`
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatal(err)
	}
	ev := (&singleContextProtocol{}).decodeEvent(raw)
	got := orderedErrorText(raw, text, ev.err)
	if want := `{"type":"quota","detail":"a<b>&c","code":7}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestToElevenLabsStreamOutputFormat_ErrorQuotesLikeJSONStringify(t *testing.T) {
	_, err := toElevenLabsStreamOutputFormat(&provider.AudioFormat{Type: "audio/x<é>\t\x01\u2028"})
	// JSON.stringify: \u0001 (Go %q would write \x01), U+2028 raw, no HTML escaping.
	want := "ElevenLabs streaming speech does not support the output format {\"type\":\"audio/x<é>\\t\\u0001\u2028\"}."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want it to contain %s", err, want)
	}
}
