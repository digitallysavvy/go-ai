package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestGoogleSpeechModelRequestAndWAVResponse(t *testing.T) {
	var capturedPath string
	var capturedKey string
	var capturedUserAgent string
	var capturedBody map[string]interface{}
	pcm := []byte{1, 0, 2, 0}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedKey = r.Header.Get("x-goog-api-key")
		capturedUserAgent = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Google-Request", "speech-1")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"` + base64.StdEncoding.EncodeToString(pcm) + `"}}]}}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.SpeechModel(ModelGemini25FlashTTS)
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	speed := 1.2
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "hello",
		Instructions: "Say warmly",
		Speed:        &speed,
		Language:     "en",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if capturedPath != "/models/gemini-2.5-flash-preview-tts:generateContent" {
		t.Fatalf("path = %q", capturedPath)
	}
	if capturedKey != "test-key" {
		t.Fatalf("x-goog-api-key = %q", capturedKey)
	}
	if capturedUserAgent != "go-ai/google/0.5.0" {
		t.Fatalf("User-Agent = %q", capturedUserAgent)
	}
	contents := capturedBody["contents"].([]interface{})
	part := contents[0].(map[string]interface{})["parts"].([]interface{})[0].(map[string]interface{})
	if part["text"] != "Say warmly: hello" {
		t.Fatalf("text = %#v", part["text"])
	}
	gen := capturedBody["generationConfig"].(map[string]interface{})
	modalities := gen["responseModalities"].([]interface{})
	if modalities[0] != "AUDIO" {
		t.Fatalf("responseModalities = %#v", modalities)
	}
	voice := gen["speechConfig"].(map[string]interface{})["voiceConfig"].(map[string]interface{})["prebuiltVoiceConfig"].(map[string]interface{})["voiceName"]
	if voice != defaultGeminiTTSVoice {
		t.Fatalf("voice = %#v", voice)
	}
	if len(result.Audio) != 44+len(pcm) || string(result.Audio[:4]) != "RIFF" {
		t.Fatalf("audio result len=%d", len(result.Audio))
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	meta := result.ProviderMetadata["google"].(map[string]interface{})
	if meta["sampleRate"] != 24000 || meta["mimeType"] != "audio/L16;rate=24000" {
		t.Fatalf("metadata = %#v", meta)
	}
	if _, ok := meta["headers"]; ok {
		t.Fatalf("provider metadata must not contain response headers: %#v", meta)
	}
	if result.Request.Body == "" {
		t.Fatal("expected request body metadata")
	}
	if result.Response == nil || result.Response.ModelID != ModelGemini25FlashTTS || result.Response.Headers["X-Google-Request"] != "speech-1" {
		t.Fatalf("response metadata = %#v", result.Response)
	}
	if result.Response.Body == nil {
		t.Fatal("expected response body metadata")
	}
	body, ok := result.Response.Body.(map[string]interface{})
	if !ok || body["candidates"] == nil {
		t.Fatalf("response body = %#v, want parsed JSON object", result.Response.Body)
	}
}

func TestGoogleSpeechModelMultiSpeakerAndPCM(t *testing.T) {
	var capturedBody map[string]interface{}
	pcm := []byte{1, 2, 3, 4}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=16000","data":"` + base64.StdEncoding.EncodeToString(pcm) + `"}}]}}]}`))
	}))
	defer server.Close()

	model := NewSpeechModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25ProTTS)
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Joe: Hi\nJane: Hello",
		Instructions: "ignored",
		OutputFormat: "pcm",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"multiSpeakerVoiceConfig": map[string]interface{}{
					"ignored": "stripped",
					"speakerVoiceConfigs": []interface{}{
						map[string]interface{}{
							"speaker": "Joe",
							"ignored": "stripped",
							"voiceConfig": map[string]interface{}{
								"ignored":             "stripped",
								"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore", "ignored": "stripped"},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	gen := capturedBody["generationConfig"].(map[string]interface{})
	multiSpeaker := gen["speechConfig"].(map[string]interface{})["multiSpeakerVoiceConfig"].(map[string]interface{})
	if multiSpeaker == nil {
		t.Fatalf("multiSpeakerVoiceConfig missing: %#v", gen["speechConfig"])
	}
	if _, ok := multiSpeaker["ignored"]; ok {
		t.Fatalf("unknown multiSpeakerVoiceConfig keys were not stripped: %#v", multiSpeaker)
	}
	speaker := multiSpeaker["speakerVoiceConfigs"].([]interface{})[0].(map[string]interface{})
	if _, ok := speaker["ignored"]; ok {
		t.Fatalf("unknown speaker config keys were not stripped: %#v", speaker)
	}
	voiceConfig := speaker["voiceConfig"].(map[string]interface{})
	if _, ok := voiceConfig["ignored"]; ok {
		t.Fatalf("unknown voiceConfig keys were not stripped: %#v", voiceConfig)
	}
	prebuilt := voiceConfig["prebuiltVoiceConfig"].(map[string]interface{})
	if _, ok := prebuilt["ignored"]; ok {
		t.Fatalf("unknown prebuiltVoiceConfig keys were not stripped: %#v", prebuilt)
	}
	text := capturedBody["contents"].([]interface{})[0].(map[string]interface{})["parts"].([]interface{})[0].(map[string]interface{})["text"]
	if text != "Joe: Hi\nJane: Hello" {
		t.Fatalf("multi-speaker text = %#v", text)
	}
	if string(result.Audio) != string(pcm) {
		t.Fatalf("pcm result = %v", result.Audio)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestGoogleSpeechModelParsesProviderErrorPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Google-Request", "bad-1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad speech request","status":"INVALID_ARGUMENT"}}`))
	}))
	defer server.Close()

	model := NewSpeechModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini25FlashTTS)
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T %v", err, err)
	}
	if providerErr.StatusCode != http.StatusBadRequest || providerErr.ErrorCode != "INVALID_ARGUMENT" || providerErr.Message != "bad speech request" {
		t.Fatalf("provider error = %#v", providerErr)
	}
	if providerErr.ResponseHeaders["X-Google-Request"] != "bad-1" {
		t.Fatalf("response headers = %#v", providerErr.ResponseHeaders)
	}
}

func TestGoogleSpeechModelInvalidProviderOptionsUseInvalidArgumentError(t *testing.T) {
	model := NewSpeechModel(New(Config{APIKey: "k", BaseURL: "https://example.test"}), ModelGemini25FlashTTS)
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hello",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"multiSpeakerVoiceConfig": "bad",
			},
		},
	})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidArgumentError, got %T %v", err, err)
	}
	if invalid.Field != "providerOptions" || invalid.Message != "invalid google provider options" {
		t.Fatalf("invalid argument error = %#v", invalid)
	}
}
