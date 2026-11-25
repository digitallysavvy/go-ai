package assemblyai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS packages/assemblyai/src/assemblyai-speech-model.test.ts
// (e6ad3867b8) and the changed cases of assemblyai-transcription-model.test.ts
// (e6ad3867b8, dd89e17ab2).

type portprovcSpeechCall struct {
	headers http.Header
	raw     string
	body    map[string]interface{}
}

type portprovcSpeechServer struct {
	mu    sync.Mutex
	calls []portprovcSpeechCall
}

// portprovcSpeechSetup serves /v1/speech with the given status, response
// headers and body, and returns a provider whose speech host is the server.
func portprovcSpeechSetup(t *testing.T, status int, headers map[string]string, body []byte, cfg Config) (*portprovcSpeechServer, *Provider) {
	t.Helper()
	s := &portprovcSpeechServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		s.mu.Lock()
		s.calls = append(s.calls, portprovcSpeechCall{headers: r.Header.Clone(), raw: string(raw), body: b})
		s.mu.Unlock()
		if r.URL.Path != "/v1/speech" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("content-type", "audio/mpeg")
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	cfg.APIKey = "test-api-key"
	cfg.SpeechBaseURL = srv.URL + "/"
	return s, New(cfg)
}

func portprovcSpeechModel(t *testing.T, p *Provider) provider.SpeechModel {
	t.Helper()
	m, err := p.SpeechModel(SpeechModelTTS1)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func portprovcAudio() []byte { return make([]byte, 100) }

func portprovcSpeechBody(t *testing.T, headers map[string]string, opts provider.SpeechGenerateOptions) (*portprovcSpeechServer, *types.SpeechResult) {
	t.Helper()
	s, p := portprovcSpeechSetup(t, 200, headers, portprovcAudio(), Config{})
	res, err := portprovcSpeechModel(t, p).DoGenerate(context.Background(), &opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, res
}

// 'should send a buffered mp3 request by default'
func TestPortProvcSpeechDefaultRequest(t *testing.T) {
	s, res := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!"})
	want := map[string]interface{}{"text": "Hello, world!", "model_id": "assemblyai-tts-1", "encoding": "mp3", "stream": false}
	if !reflect.DeepEqual(s.calls[0].body, want) {
		t.Fatalf("body = %v", s.calls[0].body)
	}
	// TS requestBody key order, and request.body is JSON.stringify of it.
	if s.calls[0].raw != `{"text":"Hello, world!","model_id":"assemblyai-tts-1","encoding":"mp3","stream":false}` {
		t.Fatalf("raw body = %s", s.calls[0].raw)
	}
	if res.Request.Body != s.calls[0].raw {
		t.Fatalf("request body = %s", res.Request.Body)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	if res.Usage != nil {
		t.Fatalf("usage = %v, want nil without a billed-characters header", res.Usage)
	}
}

// 'should pass the API key, custom headers, and user agent'
func TestPortProvcSpeechHeaders(t *testing.T) {
	s, p := portprovcSpeechSetup(t, 200, nil, portprovcAudio(), Config{Headers: map[string]string{"Custom-Provider-Header": "provider-header-value"}})
	if _, err := portprovcSpeechModel(t, p).DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, world!", Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	}); err != nil {
		t.Fatal(err)
	}
	h := s.calls[0].headers
	for k, v := range map[string]string{
		"authorization": "test-api-key", "content-type": "application/json",
		"custom-provider-header": "provider-header-value", "custom-request-header": "request-header-value",
	} {
		if h.Get(k) != v {
			t.Errorf("header %s = %q, want %q", k, h.Get(k), v)
		}
	}
	if !strings.Contains(h.Get("user-agent"), "ai-sdk-assemblyai/") {
		t.Errorf("user-agent = %q", h.Get("user-agent"))
	}
}

// 'should send the voice and translate the language code' and 'should
// accept a full language name'
func TestPortProvcSpeechVoiceAndLanguage(t *testing.T) {
	s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hola, mundo!", Voice: "aai-kyla", Language: "es"})
	if s.calls[0].body["voice_id"] != "aai-kyla" || s.calls[0].body["language"] != "spanish" {
		t.Fatalf("body = %v", s.calls[0].body)
	}
	// Key order: voice_id before language, after stream.
	if !strings.HasSuffix(s.calls[0].raw, `"stream":false,"voice_id":"aai-kyla","language":"spanish"}`) {
		t.Fatalf("raw = %s", s.calls[0].raw)
	}
	s, _ = portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Bonjour", Language: "French"})
	if s.calls[0].body["language"] != "french" {
		t.Fatalf("body = %v", s.calls[0].body)
	}
}

// it.each output formats.
func TestPortProvcSpeechOutputFormats(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"wav":   {"encoding": "pcm_s16le", "container": "wav"},
		"pcm":   {"encoding": "pcm_s16le", "container": "raw"},
		"mulaw": {"encoding": "pcm_mulaw", "container": "raw", "sample_rate": 8000.0},
		"alaw":  {"encoding": "pcm_alaw", "container": "raw", "sample_rate": 8000.0},
	}
	for format, expected := range cases {
		t.Run(format, func(t *testing.T) {
			s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!", OutputFormat: format})
			want := map[string]interface{}{"text": "Hello, world!", "model_id": "assemblyai-tts-1", "stream": false}
			for k, v := range expected {
				want[k] = v
			}
			if !reflect.DeepEqual(s.calls[0].body, want) {
				t.Fatalf("body = %v\nwant %v", s.calls[0].body, want)
			}
		})
	}
	// The format is case-insensitive.
	s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "x", OutputFormat: "WAV"})
	if s.calls[0].body["container"] != "wav" {
		t.Fatalf("body = %v", s.calls[0].body)
	}
}

// 'should fall back to mp3 for an unknown output format'
func TestPortProvcSpeechUnknownOutputFormat(t *testing.T) {
	s, res := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!", OutputFormat: "opus"})
	if s.calls[0].body["encoding"] != "mp3" {
		t.Fatalf("body = %v", s.calls[0].body)
	}
	want := []types.Warning{{
		Type: "unsupported", Feature: "outputFormat",
		Details: `Unknown output format "opus". Falling back to mp3. Use providerOptions.assemblyai to configure encoding, container, and sampleRate directly.`,
	}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should apply provider options over the output format'
func TestPortProvcSpeechProviderOptionsOverrideFormat(t *testing.T) {
	s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{
		Text: "Hello, world!", OutputFormat: "mulaw",
		ProviderOptions: map[string]interface{}{"assemblyai": map[string]interface{}{"encoding": "pcm_s16le", "sampleRate": 16000}},
	})
	// The mulaw container and 8 kHz rate do not carry over to pcm_s16le.
	want := map[string]interface{}{"text": "Hello, world!", "model_id": "assemblyai-tts-1", "encoding": "pcm_s16le", "sample_rate": 16000.0, "stream": false}
	if !reflect.DeepEqual(s.calls[0].body, want) {
		t.Fatalf("body = %v", s.calls[0].body)
	}
}

// A typed SpeechModelOptions value works like the map form.
func TestPortProvcSpeechTypedOptions(t *testing.T) {
	rate := 48000
	s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{
		Text: "x", ProviderOptions: map[string]interface{}{"assemblyai": SpeechModelOptions{Encoding: "pcm_s16le", Container: "wav", SampleRate: &rate}},
	})
	if s.calls[0].body["sample_rate"] != 48000.0 || s.calls[0].body["container"] != "wav" || s.calls[0].body["encoding"] != "pcm_s16le" {
		t.Fatalf("body = %v", s.calls[0].body)
	}
}

// 'should send the bit rate for mp3 output'
func TestPortProvcSpeechBitRate(t *testing.T) {
	s, _ := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{
		Text: "Hello, world!", ProviderOptions: map[string]interface{}{"assemblyai": map[string]interface{}{"bitRate": 128000}},
	})
	if s.calls[0].body["encoding"] != "mp3" || s.calls[0].body["bit_rate"] != 128000.0 {
		t.Fatalf("body = %v", s.calls[0].body)
	}
}

// 'should ignore options that do not fit the encoding'
func TestPortProvcSpeechIgnoresMismatchedOptions(t *testing.T) {
	s1, mp3 := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{
		Text: "Hello, world!", ProviderOptions: map[string]interface{}{"assemblyai": map[string]interface{}{"container": "wav"}},
	})
	s2, wav := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{
		Text: "Hello, world!", OutputFormat: "wav", ProviderOptions: map[string]interface{}{"assemblyai": map[string]interface{}{"bitRate": 64000}},
	})
	if _, ok := s1.calls[0].body["container"]; ok {
		t.Fatalf("container sent for mp3: %v", s1.calls[0].body)
	}
	if _, ok := s2.calls[0].body["bit_rate"]; ok {
		t.Fatalf("bit_rate sent for wav: %v", s2.calls[0].body)
	}
	got := append(append([]types.Warning{}, mp3.Warnings...), wav.Warnings...)
	want := []types.Warning{
		{Type: "unsupported", Feature: "providerOptions.assemblyai.container", Details: "AssemblyAI MP3 output has no container. The container option was ignored."},
		{Type: "unsupported", Feature: "providerOptions.assemblyai.bitRate", Details: "AssemblyAI only accepts a bit rate for MP3 output. The bitRate option was ignored."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %+v", got)
	}
}

// 'should warn on unsupported language, speed, and instructions'
func TestPortProvcSpeechWarnsOnUnsupported(t *testing.T) {
	speed := 1.2
	s, res := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!", Language: "ja", Speed: &speed, Instructions: "Speak slowly"})
	for _, k := range []string{"language", "speed"} {
		if _, ok := s.calls[0].body[k]; ok {
			t.Fatalf("%s sent: %v", k, s.calls[0].body)
		}
	}
	if len(res.Warnings) != 3 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	wantLanguage := types.Warning{
		Type: "unsupported", Feature: "language",
		Details: `AssemblyAI text-to-speech does not support language "ja" (supported: de, en, es, fr, it, pt). The language option was ignored.`,
	}
	if res.Warnings[0] != wantLanguage {
		t.Fatalf("language warning = %+v", res.Warnings[0])
	}
	if res.Warnings[1].Feature != "speed" || res.Warnings[2].Feature != "instructions" ||
		res.Warnings[2].Details != "AssemblyAI text-to-speech does not support instructions. Instructions parameter was ignored." {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should omit automatic language detection with a warning'
func TestPortProvcSpeechAutoLanguage(t *testing.T) {
	s, res := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!", Language: "auto"})
	if _, ok := s.calls[0].body["language"]; ok {
		t.Fatalf("language sent: %v", s.calls[0].body)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "language", Details: "AssemblyAI text-to-speech does not detect the language. The voice speaks its own language."}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should not warn for the default speed'
func TestPortProvcSpeechDefaultSpeedNoWarning(t *testing.T) {
	one := 1.0
	_, res := portprovcSpeechBody(t, nil, provider.SpeechGenerateOptions{Text: "Hello, world!", Speed: &one})
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should return the audio, usage, and response metadata'
func TestPortProvcSpeechResult(t *testing.T) {
	before := time.Now()
	_, res := portprovcSpeechBody(t, map[string]string{
		"x-request-id": "test-request-id", "x-tts-model": "assemblyai-tts-1", "x-tts-voice-id": "aai-kyla",
		"x-tts-language": "english", "x-tts-encoding": "mp3", "x-tts-sample-rate": "24000",
		"x-tts-audio-duration-ms": "3200", "x-tts-billed-characters": "13",
	}, provider.SpeechGenerateOptions{Text: "Hello, world!"})
	if !reflect.DeepEqual(res.Audio, portprovcAudio()) {
		t.Fatalf("audio = %d bytes", len(res.Audio))
	}
	if !reflect.DeepEqual(res.Usage, map[string]interface{}{"characters": 13.0}) {
		t.Fatalf("usage = %v", res.Usage)
	}
	want := map[string]interface{}{"assemblyai": map[string]interface{}{
		"requestId": "test-request-id", "model": "assemblyai-tts-1", "voiceId": "aai-kyla", "language": "english",
		"encoding": "mp3", "sampleRate": 24000.0, "audioDurationMs": 3200.0, "billedCharacters": 13.0,
	}}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("metadata = %v", res.ProviderMetadata)
	}
	if res.Response.ModelID != "assemblyai-tts-1" || res.Response.Timestamp.Before(before) || res.Response.Headers["x-request-id"] != "test-request-id" {
		t.Fatalf("response = %+v", res.Response)
	}
}

// Non-numeric numeric headers are omitted (TS Number.isNaN).
func TestPortProvcSpeechIgnoresNaNHeaders(t *testing.T) {
	_, res := portprovcSpeechBody(t, map[string]string{"x-tts-billed-characters": "abc", "x-tts-sample-rate": "x"}, provider.SpeechGenerateOptions{Text: "x"})
	if res.Usage != nil || len(res.ProviderMetadata["assemblyai"].(map[string]interface{})) != 0 {
		t.Fatalf("usage = %v, metadata = %v", res.Usage, res.ProviderMetadata)
	}
}

// 'should surface API errors'
func TestPortProvcSpeechAPIError(t *testing.T) {
	body := `{"code":"voice_not_found","message":"voice_id: no voice with this ID or slug","param":"voice_id","request_id":"test-request-id","errors":null,"retryable":false,"retry_after_seconds":null,"error":"voice_id: no voice with this ID or slug"}`
	_, p := portprovcSpeechSetup(t, 422, map[string]string{"content-type": "application/json"}, []byte(body), Config{})
	_, err := portprovcSpeechModel(t, p).DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "missing"})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "voice_id: no voice with this ID or slug" || perr.StatusCode != 422 {
		t.Fatalf("error = %v (%T)", err, err)
	}
}

// parseProviderOptions rejects a mistyped option before any request.
func TestPortProvcSpeechRejectsInvalidOptions(t *testing.T) {
	for name, options := range map[string]map[string]interface{}{
		"encoding":   {"encoding": "flac"},
		"container":  {"container": "ogg"},
		"sampleRate": {"sampleRate": 12345},
		"bitRate":    {"bitRate": 1},
		"null":       {"encoding": nil},
	} {
		t.Run(name, func(t *testing.T) {
			s, p := portprovcSpeechSetup(t, 200, nil, portprovcAudio(), Config{})
			_, err := portprovcSpeechModel(t, p).DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
				Text: "x", ProviderOptions: map[string]interface{}{"assemblyai": options},
			})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v (%T)", err, err)
			}
			if len(s.calls) != 0 {
				t.Fatalf("calls = %d", len(s.calls))
			}
		})
	}
}

func TestPortProvcSpeechModelIdentity(t *testing.T) {
	m := portprovcSpeechModel(t, New(Config{APIKey: "k"}))
	if m.Provider() != "assemblyai.speech" || m.ModelID() != "assemblyai-tts-1" || m.SpecificationVersion() != "v4" {
		t.Fatalf("model = %s %s", m.Provider(), m.ModelID())
	}
	restored, err := provider.DeserializeSpeechModel(m.(*SpeechModel).Serialize())
	if err != nil || restored.Provider() != "assemblyai.speech" {
		t.Fatalf("restored = %v, %v", restored, err)
	}
	// The default speech host is the TTS API.
	if DefaultSpeechBaseURL != "https://tts.us.assemblyai.com" {
		t.Fatalf("DefaultSpeechBaseURL = %s", DefaultSpeechBaseURL)
	}
}

// --- transcription ---

type portprovcTranscriptionServer struct {
	mu       sync.Mutex
	upload   []byte
	submit   map[string]interface{}
	srv      *httptest.Server
	provider *Provider
}

func portprovcTranscriptionSetup(t *testing.T) *portprovcTranscriptionServer {
	t.Helper()
	s := &portprovcTranscriptionServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.upload = b
		s.mu.Unlock()
		_, _ = io.WriteString(w, `{"upload_url":"https://storage.assemblyai.com/mock-upload-url"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		s.mu.Lock()
		s.submit = b
		s.mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"9ea68fd3-f953-42c1-9742-976c70f2c0b5","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/9ea68fd3-f953-42c1-9742-976c70f2c0b5", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"9ea68fd3-f953-42c1-9742-976c70f2c0b5","status":"completed","text":"Hello, world!","words":[],"audio_duration":1}`)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	s.provider = New(Config{APIKey: "test-api-key", BaseURL: s.srv.URL})
	return s
}

func portprovcTranscribe(t *testing.T, s *portprovcTranscriptionServer, modelID string, opts *provider.TranscriptionOptions) *types.TranscriptionResult {
	t.Helper()
	m, err := s.provider.TranscriptionModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{Audio: []byte("audio"), MimeType: "audio/wav"}
	}
	res, err := m.DoTranscribe(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

const portprovcDocs = "https://www.assemblyai.com/docs/pre-recorded-audio/select-the-speech-model"

// 'should pass newer models via the speech_models parameter'
func TestPortProvcTranscriptionFlagshipUsesSpeechModels(t *testing.T) {
	s := portprovcTranscriptionSetup(t)
	res := portprovcTranscribe(t, s, "assemblyai-stt-3-7", nil)
	if s.submit["audio_url"] != "https://storage.assemblyai.com/mock-upload-url" ||
		!reflect.DeepEqual(s.submit["speech_models"], []interface{}{"assemblyai-stt-3-7"}) {
		t.Fatalf("submit = %v", s.submit)
	}
	if _, ok := s.submit["speech_model"]; ok {
		t.Fatalf("speech_model sent: %v", s.submit)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should route deprecated universal-3-5-pro via speech_models with a
// deprecation warning'
func TestPortProvcTranscriptionDeprecatedUniversal35Pro(t *testing.T) {
	s := portprovcTranscriptionSetup(t)
	res := portprovcTranscribe(t, s, "universal-3-5-pro", nil)
	if !reflect.DeepEqual(s.submit["speech_models"], []interface{}{"universal-3-5-pro"}) {
		t.Fatalf("submit = %v", s.submit)
	}
	want := []types.Warning{{
		Type: "deprecated", Setting: "model 'universal-3-5-pro'",
		Message: "AssemblyAI has deprecated 'universal-3-5-pro'. Use 'assemblyai-stt-3-7' instead. See documentation: " + portprovcDocs,
	}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// 'should nudge universal-2 users toward assemblyai-stt-3-7 without
// deprecating it'
func TestPortProvcTranscriptionUniversal2Nudge(t *testing.T) {
	s := portprovcTranscriptionSetup(t)
	res := portprovcTranscribe(t, s, "universal-2", nil)
	want := []types.Warning{{
		Type:    "other",
		Message: "'assemblyai-stt-3-7' is AssemblyAI's latest flagship model; 'universal-2' remains available for languages it does not support. See " + portprovcDocs,
	}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// it.each(['best', 'slam-1', 'nano', 'universal-3-pro']): 'should not
// special-case the removed %s model'
func TestPortProvcTranscriptionRemovedModelsPassThrough(t *testing.T) {
	for _, id := range []string{"best", "slam-1", "nano", "universal-3-pro"} {
		t.Run(id, func(t *testing.T) {
			s := portprovcTranscriptionSetup(t)
			res := portprovcTranscribe(t, s, id, nil)
			if !reflect.DeepEqual(s.submit["speech_models"], []interface{}{id}) {
				t.Fatalf("submit = %v", s.submit)
			}
			if _, ok := s.submit["speech_model"]; ok {
				t.Fatalf("speech_model sent: %v", s.submit)
			}
			if len(res.Warnings) != 0 {
				t.Fatalf("warnings = %+v", res.Warnings)
			}
		})
	}
}

// The renamed deprecation text for wordBoost / boostParam.
func TestPortProvcTranscriptionWordBoostDeprecationText(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), ModelAssemblyAISTT37)
	_, warnings := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{
		"assemblyai": map[string]interface{}{"wordBoost": []interface{}{"a"}},
	}})
	want := types.Warning{
		Type: "deprecated", Setting: "wordBoost",
		Message: "'wordBoost' and 'boostParam' are deprecated and are rejected by 'assemblyai-stt-3-7' and 'universal-3-5-pro'. Use 'keytermsPrompt' instead.",
	}
	if len(warnings) != 1 || warnings[0] != want {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// An empty model ID selects the flagship, not the removed "best".
func TestPortProvcTranscriptionEmptyModelIDDefault(t *testing.T) {
	m, _ := New(Config{APIKey: "k"}).TranscriptionModel("")
	if m.ModelID() != "assemblyai-stt-3-7" {
		t.Fatalf("model ID = %q", m.ModelID())
	}
}
