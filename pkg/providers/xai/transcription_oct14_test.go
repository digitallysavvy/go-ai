package xai

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from TS #21981 and #22048.

func xaiBatchCapture(t *testing.T, responseBody string) (*httptest.Server, func() ([]string, map[string]string)) {
	t.Helper()
	var rawBody []byte
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		rawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server, func() ([]string, map[string]string) {
		reader := multipart.NewReader(bytes.NewReader(rawBody), strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
		var order []string
		values := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			order = append(order, part.FormName())
			values[part.FormName()] = string(data)
			if part.FormName() == "file" {
				values["filename"] = part.FileName()
			}
		}
		return order, values
	}
}

func TestXAITranscriptionBatchSendsModelAndVadThresholdBeforeFile(t *testing.T) {
	server, captured := xaiBatchCapture(t, `{"text":"Hello from the AI SDK!"}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v1"})
	model, _ := p.TranscriptionModel("grok-voice-transcribe-2.0")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"vadThreshold": 0.3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	order, values := captured()
	if !reflect.DeepEqual(order, []string{"model", "vad_threshold", "file"}) {
		t.Errorf("order = %v", order)
	}
	if values["model"] != "grok-voice-transcribe-2.0" || values["vad_threshold"] != "0.3" {
		t.Errorf("values = %v", values)
	}
	if result.Response.ModelID != "grok-voice-transcribe-2.0" {
		t.Errorf("response model = %q", result.Response.ModelID)
	}
	if result.ProviderMetadata != nil {
		t.Errorf("providerMetadata = %v, want nil", result.ProviderMetadata)
	}
}

func TestXAITranscriptionBatchOmitsModelWhenUnset(t *testing.T) {
	server, captured := xaiBatchCapture(t, `{"text":"x"}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v1"})
	model, _ := p.TranscriptionModel("")
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	if order, _ := captured(); !reflect.DeepEqual(order, []string{"file"}) {
		t.Errorf("order = %v", order)
	}
}

func TestXAITranscriptionBatchOpusAudioFormat(t *testing.T) {
	server, captured := xaiBatchCapture(t, `{"text":"x"}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v1"})
	model, _ := p.TranscriptionModel("")
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/ogg",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"audioFormat": "opus"}},
	}); err != nil {
		t.Fatal(err)
	}
	_, values := captured()
	if values["audio_format"] != "opus" || values["filename"] != "audio.ogg" {
		t.Errorf("values = %v", values)
	}
}

func TestXAITranscriptionVadThresholdValidation(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, _ := p.TranscriptionModel("")
	for _, v := range []float64{-0.1, 1.1} {
		_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte{1}, ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"vadThreshold": v}},
		})
		if err == nil {
			t.Errorf("vadThreshold %v: want error", v)
		}
	}
}

func TestXAITranscriptionPreservesDiarizedWordsInProviderMetadata(t *testing.T) {
	server, _ := xaiBatchCapture(t, `{"text":"Hello from the AI SDK!","language":"en","duration":2.5,"words":[{"text":"Hello","start":0,"end":1,"speaker":0},{"text":"from the AI SDK!","start":1,"end":2.5,"speaker":1}]}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v1"})
	model, _ := p.TranscriptionModel("")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"diarize": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"xai": map[string]interface{}{"words": []map[string]interface{}{
		{"text": "Hello", "start": 0.0, "end": 1.0, "speaker": 0.0},
		{"text": "from the AI SDK!", "start": 1.0, "end": 2.5, "speaker": 1.0},
	}}}
	if !reflect.DeepEqual(result.ProviderMetadata, want) {
		t.Errorf("providerMetadata = %#v, want %#v", result.ProviderMetadata, want)
	}
}

func TestXAITranscriptionModelSerializationKeepsModelID(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewTranscriptionModel(p, "grok-voice-transcribe-2.0")
	restored, err := deserializeTranscriptionModel(model.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if restored.ModelID() != "grok-voice-transcribe-2.0" {
		t.Errorf("restored model ID = %q", restored.ModelID())
	}
}

func xaiStreamURL(t *testing.T, modelID string, opts *provider.TranscriptionStreamOptions) (url.Values, []string) {
	t.Helper()
	server := newXAISTTTestServer(t)
	defer server.close()
	p := New(Config{APIKey: "test-api-key", BaseURL: server.ts.URL})
	model := NewTranscriptionModel(p, modelID)
	opts.Audio = newXAIChanAudioStream([]byte{1, 2, 3})
	result, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck
	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()
	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)
	dones := 1
	if xaiOpts, ok := opts.ProviderOptions["xai"].(map[string]interface{}); ok && xaiOpts["multichannel"] == true {
		dones = xaiOpts["channels"].(int)
	}
	for i := 0; i < dones; i++ {
		server.send(map[string]interface{}{"type": "transcript.done", "text": "", "duration": 0, "channel_index": i})
	}
	parts := <-partsCh
	var warnings []string
	if len(parts) > 0 {
		for _, w := range parts[0].Warnings {
			warnings = append(warnings, w.Message)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.requestURL.Query(), warnings
}

func TestXAITranscriptionStreamSendsModelAndVadThreshold(t *testing.T) {
	q, _ := xaiStreamURL(t, "grok-voice-transcribe-2.0", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions:  map[string]interface{}{"xai": map[string]interface{}{"vadThreshold": 0.3}},
	})
	if q.Get("model") != "grok-voice-transcribe-2.0" || q.Get("vad_threshold") != "0.3" || q.Get("encoding") != "pcm" || q.Get("sample_rate") != "16000" {
		t.Errorf("query = %v", q)
	}

	q, _ = xaiStreamURL(t, "", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if _, ok := q["model"]; ok {
		t.Errorf("model param sent without a model id: %v", q)
	}
}

func TestXAITranscriptionStreamOpus(t *testing.T) {
	q, warnings := xaiStreamURL(t, "", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/opus", Rate: intPtr(48000)},
	})
	if q.Get("encoding") != "opus" || q.Get("sample_rate") != "48000" || len(warnings) != 0 {
		t.Errorf("query = %v warnings = %v", q, warnings)
	}

	q, _ = xaiStreamURL(t, "", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(48000)},
		ProviderOptions:  map[string]interface{}{"xai": map[string]interface{}{"audioFormat": "opus", "sampleRate": 16000}},
	})
	if q.Get("encoding") != "opus" || q.Get("sample_rate") != "16000" {
		t.Errorf("explicit audioFormat query = %v", q)
	}

	q, _ = xaiStreamURL(t, "", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/opus", Rate: intPtr(48000)},
		ProviderOptions:  map[string]interface{}{"xai": map[string]interface{}{"multichannel": true, "channels": 2}},
	})
	if q.Get("encoding") != "opus" || q.Get("multichannel") != "true" || q.Get("channels") != "2" || q.Get("sample_rate") != "48000" {
		t.Errorf("multichannel opus query = %v", q)
	}
}

func TestXAITranscriptionStreamUnrecognizedFormatWarningListsOpus(t *testing.T) {
	_, warnings := xaiStreamURL(t, "", &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/wav"},
	})
	want := `Unrecognized inputAudioFormat.type "audio/wav"; falling back to raw PCM encoding. Use audio/pcm, audio/pcmu, audio/pcma, audio/opus, or set providerOptions.xai.audioFormat explicitly.`
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %v", warnings)
	}
}
