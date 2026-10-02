package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// --- routing / default API resolution -------------------------------------------------

func TestAzureTranscription_RoutesMAITranscribe2ToSpeech(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("mai-transcribe-2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	dispatch := model.(*azureTranscriptionDispatchModel)
	resolved, err := dispatch.resolvedAPI(nil)
	if err != nil {
		t.Fatalf("resolvedAPI: %v", err)
	}
	if resolved.API != AzureTranscriptionAPISpeech {
		t.Fatalf("API = %q, want speech", resolved.API)
	}
}

func TestAzureTranscription_RoutesMAITranscribe15ToSpeech(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("MAI-Transcribe-1.5") // case-insensitive
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	dispatch := model.(*azureTranscriptionDispatchModel)
	resolved, err := dispatch.resolvedAPI(nil)
	if err != nil {
		t.Fatalf("resolvedAPI: %v", err)
	}
	if resolved.API != AzureTranscriptionAPISpeech {
		t.Fatalf("API = %q, want speech", resolved.API)
	}
}

func TestAzureTranscription_RoutesStreamingModelToMAI(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("mai-transcribe-2-streaming")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	dispatch := model.(*azureTranscriptionDispatchModel)
	resolved, err := dispatch.resolvedAPI(nil)
	if err != nil {
		t.Fatalf("resolvedAPI: %v", err)
	}
	if resolved.API != AzureTranscriptionAPIMai {
		t.Fatalf("API = %q, want mai", resolved.API)
	}
}

func TestAzureTranscription_DefaultsOtherModelsToOpenAI(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("whisper-1")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	dispatch := model.(*azureTranscriptionDispatchModel)
	resolved, err := dispatch.resolvedAPI(nil)
	if err != nil {
		t.Fatalf("resolvedAPI: %v", err)
	}
	if resolved.API != AzureTranscriptionAPIOpenAI {
		t.Fatalf("API = %q, want openai", resolved.API)
	}
}

func TestAzureTranscription_APIOverrideWins(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("mai-transcribe-2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	dispatch := model.(*azureTranscriptionDispatchModel)
	resolved, err := dispatch.resolvedAPI(map[string]interface{}{"azure": map[string]interface{}{"api": "openai"}})
	if err != nil {
		t.Fatalf("resolvedAPI: %v", err)
	}
	if resolved.API != AzureTranscriptionAPIOpenAI {
		t.Fatalf("API = %q, want openai (explicit override)", resolved.API)
	}
}

func TestAzureSpeech_RoutesMAIVoiceModelsToSpeech(t *testing.T) {
	for _, id := range []string{"mai-voice-2", "MAI-Voice-2-Flash", "mai-voice-2.1", "mai-voice-2.1-flash"} {
		if _, ok := getMAIVoiceModel(id); !ok {
			t.Errorf("getMAIVoiceModel(%q) = not found, want found", id)
		}
	}
	if _, ok := getMAIVoiceModel("tts-1"); ok {
		t.Error("getMAIVoiceModel(\"tts-1\") = found, want not found")
	}
}

func TestAzureTranscriptionProviderOptions_RejectsUnknownKey(t *testing.T) {
	_, err := parseAzureTranscriptionProviderOptions(map[string]interface{}{
		"azure": map[string]interface{}{"unknownOption": true},
	})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestAzureTranscriptionProviderOptions_RejectsDiarizationMaxSpeakers(t *testing.T) {
	_, err := parseAzureTranscriptionProviderOptions(map[string]interface{}{
		"azure": map[string]interface{}{"diarization": map[string]interface{}{"enabled": true, "maxSpeakers": float64(2)}},
	})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestAzureSpeechProviderOptions_RejectsUnknownKey(t *testing.T) {
	_, err := parseAzureSpeechProviderOptions(map[string]interface{}{
		"azure": map[string]interface{}{"unknownOption": true},
	})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestAzureTranscriptionDispatch_DoGenerateMAIRejected(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("mai-transcribe-2-streaming")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	_, err = model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1, 2, 3}})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestAzureTranscriptionDispatch_DoStreamSpeechAPIRejected(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("mai-transcribe-2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	streamer := model.(provider.TranscriptionStreamer)
	_, err = streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: newChanAudioStream()})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

// --- speechBaseURL / maiBaseURL resourceName validation --------------------------------

// TestAzureSpeechBaseURL_RejectsInvalidResourceName verifies speechBaseURL /
// maiBaseURL reject an invalid ResourceName even when an explicit BaseURL
// lets New itself succeed (BaseURL only covers the OpenAI client; it does
// not make ResourceName valid for the Speech/MAI endpoints too).
func TestAzureSpeechBaseURL_RejectsInvalidResourceName(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://proxy.example/openai", ResourceName: "user@internal:8080/#"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.speechBaseURL(); !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("speechBaseURL() err = %v, want InvalidArgumentError", err)
	}
	if _, err := p.maiBaseURL(); !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("maiBaseURL() err = %v, want InvalidArgumentError", err)
	}
}

func TestAzureSpeechBaseURL_CustomOverrideBypassesResourceName(t *testing.T) {
	p, err := New(Config{
		APIKey:        "k",
		BaseURL:       "https://proxy.example/openai",
		ResourceName:  "user@internal:8080/#",
		SpeechBaseURL: "https://eastus.api.cognitive.microsoft.com",
		MaiBaseURL:    "https://eastus.services.ai.azure.com/mai/v1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := p.speechBaseURL()
	if err != nil {
		t.Fatalf("speechBaseURL() error = %v", err)
	}
	if got != "https://eastus.api.cognitive.microsoft.com" {
		t.Fatalf("speechBaseURL() = %q", got)
	}
	gotMai, err := p.maiBaseURL()
	if err != nil {
		t.Fatalf("maiBaseURL() error = %v", err)
	}
	if gotMai != "https://eastus.services.ai.azure.com/mai/v1" {
		t.Fatalf("maiBaseURL() = %q", gotMai)
	}
}

// --- Azure Speech file transcription (non-streaming) ------------------------------------

func TestAzureSpeechTranscription_RequestAndResponseShape(t *testing.T) {
	var gotPath string
	var gotHeader string
	var gotDefinition map[string]interface{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotHeader = r.Header.Get("Ocp-Apim-Subscription-Key")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_ = json.Unmarshal([]byte(r.FormValue("definition")), &gotDefinition)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"combinedPhrases": [{"text": "hello world"}],
			"durationMilliseconds": 1500,
			"phrases": [{"text": "hello world", "offsetMilliseconds": 0, "durationMilliseconds": 1500, "locale": "en-US"}]
		}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "speech-key", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1, 2, 3}, MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if gotPath != "/speechtotext/transcriptions:transcribe?api-version=2025-10-15" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotHeader != "speech-key" {
		t.Fatalf("Ocp-Apim-Subscription-Key = %q", gotHeader)
	}
	enhanced, _ := gotDefinition["enhancedMode"].(map[string]interface{})
	if enhanced["model"] != "MAI-Transcribe-2" {
		t.Fatalf("enhancedMode.model = %v, want MAI-Transcribe-2", enhanced["model"])
	}
	modelOptions, _ := enhanced["modelOptions"].(map[string]interface{})
	if modelOptions["timestamps"] != "segment" {
		t.Fatalf("modelOptions.timestamps = %v, want segment (SDK default)", modelOptions["timestamps"])
	}
	if result.Text != "hello world" || result.Language != "en" {
		t.Fatalf("result = %#v", result)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 1.5 {
		t.Fatalf("DurationInSeconds = %v", result.DurationInSeconds)
	}
}

func TestAzureSpeechTranscription_MAITranscribe15OmitsDefaultTimestamps(t *testing.T) {
	var gotDefinition map[string]interface{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_ = json.Unmarshal([]byte(r.FormValue("definition")), &gotDefinition)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"combinedPhrases": [{"text": "hi"}], "phrases": []}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-1.5")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	enhanced, _ := gotDefinition["enhancedMode"].(map[string]interface{})
	modelOptions, _ := enhanced["modelOptions"].(map[string]interface{})
	if v, ok := modelOptions["timestamps"]; ok && v != nil {
		t.Fatalf("modelOptions.timestamps = %v, want omitted for MAI-Transcribe-1.5", v)
	}
	if enhanced["model"] != "MAI-Transcribe-1.5" {
		t.Fatalf("enhancedMode.model = %v, want MAI-Transcribe-1.5", enhanced["model"])
	}
}

func TestAzureSpeechTranscription_OpenAIBackendWarnsOnSpeechOnlyOptions(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text": "hi"}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("whisper-1")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"transcribeStyle": "clean"}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "providerOptions.azure.transcribeStyle" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported warning for transcribeStyle, got %#v", result.Warnings)
	}
}

// --- Azure Speech SSML speech synthesis -------------------------------------------------

func TestAzureSpeechSpeech_RequestShape(t *testing.T) {
	var gotPath, gotFormat, gotHeader, gotBody string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath = r.URL.Path
		gotFormat = r.Header.Get("X-Microsoft-OutputFormat")
		gotHeader = r.Header.Get("Ocp-Apim-Subscription-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("fake-audio-bytes"))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "speech-key", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.SpeechModel("mai-voice-2")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello & welcome"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotPath != "/tts/cognitiveservices/v1" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotFormat != azureDefaultOutputFormat {
		t.Fatalf("X-Microsoft-OutputFormat = %q", gotFormat)
	}
	if gotHeader != "speech-key" {
		t.Fatalf("Ocp-Apim-Subscription-Key = %q", gotHeader)
	}
	if !strings.Contains(gotBody, `voice name="en-US-Harper:MAI-Voice-2"`) {
		t.Fatalf("body = %q, want default voice with MAI-Voice-2 suffix", gotBody)
	}
	if !strings.Contains(gotBody, "Hello &amp; welcome") {
		t.Fatalf("body = %q, want XML-escaped text", gotBody)
	}
	if string(result.Audio) != "fake-audio-bytes" {
		t.Fatalf("Audio = %q", result.Audio)
	}
}

func TestAzureSpeechSpeech_LanguageSelectsDefaultVoice(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.SpeechModel("mai-voice-2-flash")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hola", Language: "es"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(gotBody, `voice name="es-MX-Valeria:MAI-Voice-2-Flash"`) {
		t.Fatalf("body = %q, want es-MX-Valeria default voice", gotBody)
	}
	for _, w := range result.Warnings {
		if w.Feature == "language" {
			t.Fatalf("unexpected language warning for a mapped language: %#v", w)
		}
	}
}

func TestAzureSpeechSpeech_OpenAIBackendWarnsOnStyleOption(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.SpeechModel("tts-1")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "hi",
		ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"style": "excited"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "providerOptions.azure.style" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported warning for style, got %#v", result.Warnings)
	}
}

// --- MAI streaming transcription over WebSocket -----------------------------------------

// chanAudioStream is a minimal provider.AudioStream test double backed by a
// channel of chunks, closed to signal EOF, mirroring the equivalent helper in
// pkg/providers/openai/transcription_stream_test.go.
type chanAudioStream struct {
	chunks chan []byte
	mu     sync.Mutex
	cancel error
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
	s.cancel = reason
}

// maiTestServer is a minimal MAI realtime WebSocket test double: it records
// every client message and lets the test script server->client events.
type maiTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}
	toSend   chan interface{}
}

func newMAITestServer() *maiTestServer {
	s := &maiTestServer{toSend: make(chan interface{}, 16)}
	handler := websocket.Server{Handler: func(conn *websocket.Conn) {
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
	}}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *maiTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *maiTestServer) waitForReceived(t *testing.T, want string, timeout time.Duration) map[string]interface{} {
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
	t.Fatalf("timed out waiting for a %q message", want)
	return nil
}

// TestAzureMaiTranscription_StreamsDeltaAndFinal exercises the MAI realtime
// protocol end-to-end over a real WebSocket: session.created -> client sends
// session.update, session.updated -> client streams audio + commits,
// server sends delta/completed -> client emits delta/final/finish.
func TestAzureMaiTranscription_StreamsDeltaAndFinal(t *testing.T) {
	server := newMAITestServer()
	defer server.close()

	p, err := New(Config{APIKey: "k", ResourceName: "r", MaiBaseURL: server.ts.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-2-streaming")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	streamer := model.(provider.TranscriptionStreamer)

	rate := 16000
	audio := newChanAudioStream([]byte{1, 2, 3, 4})
	result, err := streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	// Drain the stream concurrently, as a real consumer would: Emit blocks
	// on Next() being called, so the client's stream-start part (and
	// everything after it) would otherwise never unblock while this test
	// drives the server script below.
	type drained struct {
		sawDelta, sawFinal bool
		finishText         string
		finishDuration     *float64
		err                error
	}
	doneCh := make(chan drained, 1)
	go func() {
		var d drained
		for {
			part, nextErr := result.Stream.Next()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				d.err = nextErr
				break
			}
			switch part.Type {
			case provider.TranscriptionStreamPartTypeDelta:
				d.sawDelta = true
			case provider.TranscriptionStreamPartTypeFinish:
				d.sawFinal = true
				d.finishText = part.FinishText
				d.finishDuration = part.DurationInSeconds
			}
		}
		doneCh <- d
	}()

	server.toSend <- map[string]interface{}{"type": "session.created"}
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.updated"}

	appendMsg := server.waitForReceived(t, "input_audio_buffer.append", time.Second)
	audioB64, _ := appendMsg["audio"].(string)
	decoded, decodeErr := base64.StdEncoding.DecodeString(audioB64)
	if decodeErr != nil || string(decoded) != "\x01\x02\x03\x04" {
		t.Fatalf("decoded audio = %q, err = %v", decoded, decodeErr)
	}
	server.waitForReceived(t, "input_audio_buffer.commit", time.Second)

	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "hel"}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item-1", "transcript": "hello"}

	var d drained
	select {
	case d = <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stream to finish")
	}
	if d.err != nil {
		t.Fatalf("Next() error = %v", d.err)
	}
	sawDelta, sawFinal, finishText, finishDuration := d.sawDelta, d.sawFinal, d.finishText, d.finishDuration
	if !sawDelta {
		t.Fatal("expected a transcript-delta part")
	}
	if !sawFinal {
		t.Fatal("expected a finish part")
	}
	if finishText != "hello" {
		t.Fatalf("finish text = %q, want hello", finishText)
	}
	if finishDuration == nil || *finishDuration <= 0 {
		t.Fatalf("finish duration = %v, want > 0", finishDuration)
	}
}

func TestAzureMaiTranscription_RejectsUnsupportedAudioFormat(t *testing.T) {
	p, err := New(Config{APIKey: "k", ResourceName: "r"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-2-streaming")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	streamer := model.(provider.TranscriptionStreamer)
	_, err = streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/wav"},
	})
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}
