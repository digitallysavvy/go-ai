package google

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/net/websocket"
)

// TS google-files.ts polls with getFromApi, which tags the user-agent with
// the runtime (withUserAgentSuffix(..., getRuntimeEnvironmentUserAgent()));
// the initiation is a raw fetchFn call with getHeaders() only.
func TestR98provgFilesPollCarriesRuntimeUserAgent(t *testing.T) {
	var initUA, pollUA string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			initUA = r.Header.Get("User-Agent")
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_, _ = w.Write([]byte(`{"file":{"name":"files/a","mimeType":"text/plain","uri":"u","state":"PROCESSING"}}`))
		case "/v1beta/files/a":
			pollUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"name":"files/a","mimeType":"text/plain","uri":"u","state":"ACTIVE"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	if _, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType:       "text/plain",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if strings.Contains(initUA, rt) {
		t.Fatalf("raw-fetch initiation carries the runtime tag: %q", initUA)
	}
	if !strings.Contains(pollUA, "ai-sdk-google/") || !strings.HasSuffix(pollUA, " "+rt) {
		t.Fatalf("poll user-agent = %q, want the provider tag and %q", pollUA, rt)
	}
}

// TS google-batch.ts sends the resumable upload initiation with
// postJsonToApi, the upload with postToApi (only its own headers) and the
// results download with getFromApi: each tags the user-agent with the
// runtime, like the batch create.
func TestR98provgBatchUploadAndDownloadCarryRuntimeUserAgent(t *testing.T) {
	rt := providerutils.RuntimeEnvironmentUserAgent()
	var initUA, uploadUA, createUA, downloadUA string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
		initUA = r.Header.Get("User-Agent")
		w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session-123")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/upload/v1beta/files/session-123", func(w http.ResponseWriter, r *http.Request) {
		uploadUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"file":{"name":"files/batch-input"}}`))
	})
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		createUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	})
	mux.HandleFunc("/v1beta/batches/batch-123", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output"}}}`))
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		downloadUA = r.Header.Get("User-Agent")
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	b := p.ExperimentalBatch()
	longPrompt := strings.Repeat("a", googleBatchInlineCreationMaxBytes+1000)
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("big", "gemini-2.5-flash", longPrompt)},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	_ = stream.Close()

	want := "ai-sdk-google/" // provider tag first, then the runtime
	for name, ua := range map[string]string{"create": createUA, "init": initUA, "download": downloadUA} {
		if !strings.HasPrefix(ua, want) || !strings.HasSuffix(ua, " "+rt) {
			t.Errorf("%s user-agent = %q, want %s... %s", name, ua, want, rt)
		}
	}
	if uploadUA != rt {
		t.Errorf("upload user-agent = %q, want %q", uploadUA, rt)
	}
}

func r98provgSessionConfig() provider.RealtimeSessionConfig {
	instructions, voice, desc := "be brief", "Puck", "weather"
	return provider.RealtimeSessionConfig{
		Instructions:            &instructions,
		Voice:                   &voice,
		InputAudioTranscription: &provider.RealtimeAudioTranscriptionConfig{},
		Tools: []provider.RealtimeToolDefinition{{
			Type: "function", Name: "get_weather", Description: &desc,
			Parameters: map[string]interface{}{"type": "object"},
		}},
		ProviderOptions: map[string]interface{}{
			"google":                   map[string]interface{}{"defaultToolBehavior": "NON_BLOCKING", "translationConfig": map[string]interface{}{"targetLanguageCode": "fr"}},
			"contextWindowCompression": map[string]interface{}{"slidingWindow": map[string]interface{}{}},
		},
	}
}

// TS buildGoogleSessionConfig builds the setup object in insertion order
// (model, generationConfig, systemInstruction, tools, the transcription
// configs, then Object.assign of the other provider options) and the
// session-update frame is JSON.stringify of it.
const r98provgSetupJSON = `{"model":"models/gemini-2.0-flash-live-001","generationConfig":{"responseModalities":["AUDIO"],"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Puck"}}},"translationConfig":{"targetLanguageCode":"fr"}},"systemInstruction":{"parts":[{"text":"be brief"}]},"tools":[{"functionDeclarations":[{"name":"get_weather","description":"weather","parametersJsonSchema":{"type":"object"},"behavior":"NON_BLOCKING"}]}],"inputAudioTranscription":{},"contextWindowCompression":{"slidingWindow":{}}}`

func TestR98provgRealtimeSetupKeyOrder(t *testing.T) {
	model := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-2.0-flash-live-001")
	frame, err := model.SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-update", Config: r98provgSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"setup":` + r98provgSetupJSON + `}`; string(frame) != want {
		t.Fatalf("frame =\n%s\nwant\n%s", frame, want)
	}
}

// TS doCreateClientSecret: JSON.stringify({uses, expireTime,
// newSessionExpireTime, bidiGenerateContentSetup}).
func TestR98provgRealtimeClientSecretBodyKeyOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"name":"token"}`))
	}))
	defer srv.Close()
	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}), "gemini-2.0-flash-live-001")
	cfg := r98provgSessionConfig()
	if _, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{SessionConfig: &cfg}); err != nil {
		t.Fatal(err)
	}
	got := regexp.MustCompile(`"\d{4}-\d\d-\d\dT[0-9:.]+Z"`).ReplaceAllString(body, `"T"`)
	if want := `{"uses":0,"expireTime":"T","newSessionExpireTime":"T","bidiGenerateContentSetup":` + r98provgSetupJSON + `}`; got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// r98provgFirstFrameServer is a Live WebSocket endpoint that records the
// first client frame verbatim and then closes.
func r98provgFirstFrameServer(t *testing.T) (*httptest.Server, <-chan string) {
	t.Helper()
	first := make(chan string, 1)
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		var raw string
		if websocket.Message.Receive(conn, &raw) == nil {
			first <- raw
		}
		_ = conn.Close()
	}})
	t.Cleanup(srv.Close)
	return srv, first
}

func r98provgWaitFrame(t *testing.T, first <-chan string) string {
	t.Helper()
	select {
	case raw := <-first:
		return raw
	case <-time.After(5 * time.Second):
		t.Fatal("no setup frame")
		return ""
	}
}

// TS google-transcription-model.ts sends JSON.stringify({ setup }) with
// setup = {model, inputAudioTranscription} and the transcription config in
// buildAudioTranscriptionConfig order.
func TestR98provgLiveTranscriptionSetupKeyOrder(t *testing.T) {
	srv, first := r98provgFirstFrameServer(t)
	model := newLiveTestModel(t, srv.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"mode": "SMART", "diarization": true, "wordTimestamp": true,
			"customVocabulary": []interface{}{"Gemini"}, "languageCodes": []interface{}{"en-US"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	want := `{"setup":{"model":"models/` + ModelGemini35TranscribeLive + `","inputAudioTranscription":{"languageCodes":["en-US"],"customVocabulary":["Gemini"],"wordTimestamp":true,"diarization":true,"mode":"SMART"}}}`
	if got := r98provgWaitFrame(t, first); got != want {
		t.Fatalf("setup =\n%s\nwant\n%s", got, want)
	}
}

// TS buildGoogleLiveSpeechTranslationSetup: model, generationConfig
// {responseModalities, translationConfig {targetLanguageCode,
// echoTargetLanguage}}, inputAudioTranscription, outputAudioTranscription.
func TestR98provgSpeechTranslationSetupKeyOrder(t *testing.T) {
	srv, first := r98provgFirstFrameServer(t)
	m := newSpeechTranslationTestModel(t, srv.URL, time.Second)
	result, err := m.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
		ProviderOptions:  map[string]interface{}{"google": map[string]interface{}{"echoTargetLanguage": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	want := `{"setup":{"model":"models/gemini-3.5-live-translate-preview","generationConfig":{"responseModalities":["AUDIO"],"translationConfig":{"targetLanguageCode":"es","echoTargetLanguage":true}},"inputAudioTranscription":{},"outputAudioTranscription":{}}}`
	if got := r98provgWaitFrame(t, first); got != want {
		t.Fatalf("setup =\n%s\nwant\n%s", got, want)
	}
}
