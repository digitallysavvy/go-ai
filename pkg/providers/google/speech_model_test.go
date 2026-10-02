package google

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !strings.HasPrefix(capturedUserAgent, "ai-sdk/google/0.5.0 ") {
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

// TestGoogleSpeechModelUsageMetadata is ported from TS
// google-speech-model.test.ts "should include usage metadata" (TS
// 8c659885c5 / #21427): result.usage mirrors the response's usageMetadata
// object verbatim.
func TestGoogleSpeechModelUsageMetadata(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"` +
			base64.StdEncoding.EncodeToString(pcm) +
			`"}}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.SpeechModel(ModelGemini25FlashTTS)
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello."})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if got, want := result.Usage["promptTokenCount"], float64(10); got != want {
		t.Fatalf("Usage[promptTokenCount] = %v, want %v", got, want)
	}
	if got, want := result.Usage["candidatesTokenCount"], float64(4); got != want {
		t.Fatalf("Usage[candidatesTokenCount] = %v, want %v", got, want)
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

// TestParseGeminiSampleRate covers out-of-range / malformed sample rates in
// the API-provided mimeType, including a value that overflows an int64 (CWE
// style go/incorrect-integer-conversion regression: strconv.Atoi clamps to
// math.MaxInt64 with a non-nil error on overflow instead of returning 0, so
// parseGeminiSampleRate must check the error and bound the result itself
// rather than relying on the caller to notice).
func TestParseGeminiSampleRate(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     int
	}{
		{"typical", "audio/L16;rate=24000", 24000},
		{"no match", "audio/L16", 0},
		{"overflows int64", "audio/L16;rate=99999999999999999999999999999999", 0},
		{"exceeds uint32 range", "audio/L16;rate=4294967296", 0},
		{"at uint32 max", "audio/L16;rate=4294967295", 4294967295},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseGeminiSampleRate(tt.mimeType); got != tt.want {
				t.Fatalf("parseGeminiSampleRate(%q) = %d, want %d", tt.mimeType, got, tt.want)
			}
		})
	}
}

// TestAddGeminiWAVHeaderClampsOutOfRangeSampleRate ensures a sample rate
// that can't be represented as a uint32 (or is negative) is written as 0
// into the WAV header's sample-rate and byte-rate fields instead of
// silently truncating/wrapping via uint32(sampleRate).
func TestAddGeminiWAVHeaderClampsOutOfRangeSampleRate(t *testing.T) {
	pcm := []byte{1, 2, 3, 4}

	tests := []struct {
		name       string
		sampleRate int
		wantRate   uint32
	}{
		{"negative", -1, 0},
		{"overflows uint32", int64Max(), 0},
		{"valid", 24000, 24000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := addGeminiWAVHeader(pcm, tt.sampleRate)
			if len(out) < 28 {
				t.Fatalf("wav header too short: %d bytes", len(out))
			}
			gotRate := binary.LittleEndian.Uint32(out[24:28])
			if gotRate != tt.wantRate {
				t.Fatalf("sample rate field = %d, want %d", gotRate, tt.wantRate)
			}
			gotByteRate := binary.LittleEndian.Uint32(out[28:32])
			wantByteRate := tt.wantRate * 2 // numChannels=1, bitsPerSample=16 -> blockAlign=2
			if gotByteRate != wantByteRate {
				t.Fatalf("byte rate field = %d, want %d", gotByteRate, wantByteRate)
			}
		})
	}
}

// int64Max returns math.MaxInt64 as an int, without adding a "math" import
// just for one constant.
func int64Max() int {
	return 1<<63 - 1
}
