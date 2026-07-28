package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestXAISpeechModelRequestDefaultsAndProviderOptions(t *testing.T) {
	var seen map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tts" {
			t.Fatalf("path = %s, want /v1/tts", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Test") != "request" || r.Header.Get("User-Agent") != "go-ai/xai/0.5.0" {
			t.Fatalf("headers = %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("X-Request-Id", "req-1")
		_, _ = w.Write([]byte{1, 2, 3, 4})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1", Headers: map[string]string{"X-Provider": "provider"}})
	model, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello from the AI SDK!",
		OutputFormat: "flac",
		Headers:      map[string]string{"X-Test": "request"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"sampleRate":               44100,
				"bitRate":                  192000,
				"optimizeStreamingLatency": 1,
				"textNormalization":        true,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if !bytes.Equal(result.Audio, []byte{1, 2, 3, 4}) {
		t.Fatalf("audio = %v", result.Audio)
	}
	if result.Response == nil || result.Response.ModelID != "" || result.Response.Headers["X-Request-Id"] != "req-1" {
		t.Fatalf("response metadata = %#v", result.Response)
	}
	requestBody, _ := result.Request.Body.(string)
	if result.Request == nil || !strings.Contains(requestBody, `"voice_id":"eve"`) {
		t.Fatalf("request metadata = %#v", result.Request)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "outputFormat" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	outputFormat := seen["output_format"].(map[string]interface{})
	if seen["text"] != "Hello from the AI SDK!" || seen["voice_id"] != "eve" || seen["language"] != "auto" {
		t.Fatalf("request body = %#v", seen)
	}
	if outputFormat["codec"] != "mp3" || outputFormat["sample_rate"].(float64) != 44100 || outputFormat["bit_rate"].(float64) != 192000 {
		t.Fatalf("output_format = %#v", outputFormat)
	}
	if seen["optimize_streaming_latency"].(float64) != 1 || seen["text_normalization"] != true {
		t.Fatalf("provider options = %#v", seen)
	}
}

func TestXAISpeechWarnsAndIgnoresBitRateForNonMP3(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewSpeechModel(p, "")
	body, warnings, err := model.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		OutputFormat: "wav",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"bitRate": 192000},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}
	outputFormat := body["output_format"].(map[string]interface{})
	if _, ok := outputFormat["bit_rate"]; ok {
		t.Fatalf("bit_rate should be omitted for wav: %#v", outputFormat)
	}
	if len(warnings) != 1 || warnings[0].Feature != "providerOptions" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestXAISpeechRejectsInvalidProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewSpeechModel(p, "")
	_, _, err := model.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hello",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"sampleRate": 12345},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid argument for providerOptions: invalid xai provider options") {
		t.Fatalf("buildRequestBody error = %v", err)
	}
}

func TestXAISpeechCustomBaseURLWithoutV1MatchesTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts" {
			t.Fatalf("path = %s, want /tts", r.URL.Path)
		}
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
}

func TestXAITranscriptionMultipartRequestAndResponse(t *testing.T) {
	var rawBody []byte
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stt" {
			t.Fatalf("path = %s, want /v1/stt", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "go-ai/xai/0.5.0" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		contentType = r.Header.Get("Content-Type")
		var err error
		rawBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-2")
		_, _ = w.Write([]byte(`{"text":"Hello from the AI SDK!","language":"en","duration":2.5,"words":[{"text":"Hello","start":0,"end":1},{"text":"from the AI SDK!","start":1,"end":2.5}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model, err := p.TranscriptionModel("")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/pcm",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"audioFormat":  "pcm",
				"sampleRate":   16000,
				"language":     "en",
				"format":       true,
				"multichannel": true,
				"channels":     2,
				"diarize":      true,
				"keyterm":      []string{"AI SDK", "Grok"},
				"fillerWords":  true,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if result.Text != "Hello from the AI SDK!" || result.Language != "en" || result.DurationInSeconds == nil || *result.DurationInSeconds != 2.5 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Segments) != 2 || result.Segments[1].Text != "from the AI SDK!" {
		t.Fatalf("segments = %#v", result.Segments)
	}
	if result.Response == nil || result.Response.ModelID != "" || result.Response.Headers["X-Request-Id"] != "req-2" {
		t.Fatalf("response metadata = %#v", result.Response)
	}

	reader := multipart.NewReader(bytes.NewReader(rawBody), strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
	order := []string{}
	values := map[string][]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		order = append(order, part.FormName())
		data, _ := io.ReadAll(part)
		if part.FormName() != "file" {
			values[part.FormName()] = append(values[part.FormName()], string(data))
		} else if part.FileName() != "audio.pcm" || part.Header.Get("Content-Type") != "audio/pcm" {
			t.Fatalf("file part filename/content-type = %q/%q", part.FileName(), part.Header.Get("Content-Type"))
		}
	}
	wantOrder := []string{"audio_format", "sample_rate", "language", "format", "multichannel", "channels", "diarize", "filler_words", "keyterm", "keyterm", "file"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("multipart order = %v, want %v", order, wantOrder)
	}
	if values["keyterm"][0] != "AI SDK" || values["keyterm"][1] != "Grok" || values["channels"][0] != "2" {
		t.Fatalf("multipart values = %#v", values)
	}
}

func TestXAITranscriptionCustomBaseURLWithoutV1MatchesTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stt" {
			t.Fatalf("path = %s, want /stt", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.TranscriptionModel("")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1},
		MimeType: "audio/wav",
	}); err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
}

func TestXAITranscriptionRejectsInvalidProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewTranscriptionModel(p, "")
	_, _, err := model.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte{1},
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"channels": 1},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid argument for providerOptions: invalid xai provider options") {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
}

func TestXAITranscriptionAudioExtensionMatchesTypeScript(t *testing.T) {
	tests := map[string]string{
		"audio/mpeg":  "mp3",
		"audio/mp3":   "mp3",
		"AUDIO/MPEG":  "mp3",
		"AUDIO/MP3":   "mp3",
		"audio/wav":   "wav",
		"audio/x-wav": "wav",
		"audio/wave":  "wave",
		"audio/webm":  "webm",
		"audio/ogg":   "ogg",
		"audio/opus":  "ogg",
		"audio/mp4":   "m4a",
		"audio/x-m4a": "m4a",
		"audio/flac":  "flac",
		"audio/aac":   "aac",
		"audio/pcm":   "pcm",
		"nope":        "",
	}
	for mediaType, want := range tests {
		if got := xaiAudioExtension(mediaType); got != want {
			t.Fatalf("xaiAudioExtension(%q) = %q, want %q", mediaType, got, want)
		}
	}
}

func TestXAITranscriptionAudioBase64AcceptsBase64URL(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewTranscriptionModel(p, "")
	body, contentType, err := model.buildMultipartBody(&provider.TranscriptionOptions{
		AudioBase64: "__8",
		MimeType:    "audio/wav",
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	var rawBody []byte
	if reader, ok := body.(*bytes.Buffer); ok {
		rawBody = reader.Bytes()
	} else {
		t.Fatalf("body = %T, want *bytes.Buffer", body)
	}
	mr := multipart.NewReader(bytes.NewReader(rawBody), strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.FormName() == "file" {
			data, _ := io.ReadAll(part)
			if !bytes.Equal(data, []byte{0xff, 0xff}) {
				t.Fatalf("decoded file bytes = %v, want [255 255]", data)
			}
			return
		}
	}
	t.Fatal("missing file part")
}

func TestXAITranscriptionHandlesMissingOptionalResponseFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Hello from the AI SDK!","language":""}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewTranscriptionModel(p, "")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if result.Text != "Hello from the AI SDK!" || result.Language != "" || result.DurationInSeconds != nil || len(result.Segments) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("result = %#v", result)
	}
}
