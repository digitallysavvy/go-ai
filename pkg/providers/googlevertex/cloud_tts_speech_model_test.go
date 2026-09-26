package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from ai/packages/google-vertex/src/google-vertex-cloud-tts-speech-model.test.ts
// (6d9951b: Chirp 3: HD voices on Google Cloud Text-to-Speech).

// 8 bytes of audio; base64 -> "AQIDBAUGBwg=".
var chirpAudioBytes = []byte{1, 2, 3, 4, 5, 6, 7, 8}

const chirpAudioBase64 = "AQIDBAUGBwg="

func newChirpTestServer(t *testing.T, body map[string]interface{}) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		if body == nil {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	return server, &captured
}

func newChirpProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	p, err := New(Config{
		Project:         "test-project",
		Location:        "us-central1",
		AccessToken:     "test-token",
		CloudTTSBaseURL: baseURL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
}

func TestCloudTTS_SendsTextDefaultVoiceAndLinear16(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, err := p.SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	if _, ok := model.(*CloudTTSSpeechModel); !ok {
		t.Fatalf("SpeechModel(%q) = %T, want *CloudTTSSpeechModel", "chirp-3-hd", model)
	}

	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello from the AI SDK!"})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if (*captured)["input"].(map[string]interface{})["text"] != "Hello from the AI SDK!" {
		t.Fatalf("input = %#v", (*captured)["input"])
	}
	voice := (*captured)["voice"].(map[string]interface{})
	if voice["languageCode"] != "en-US" || voice["name"] != "en-US-Chirp3-HD-Kore" {
		t.Fatalf("voice = %#v", voice)
	}
	audioConfig := (*captured)["audioConfig"].(map[string]interface{})
	if audioConfig["audioEncoding"] != "LINEAR16" {
		t.Fatalf("audioConfig = %#v", audioConfig)
	}
	if _, hasRate := audioConfig["speakingRate"]; hasRate {
		t.Fatalf("audioConfig should not include speakingRate by default: %#v", audioConfig)
	}
}

func TestCloudTTS_ComposesVoiceFromLanguageAndVoice(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hallo!", Voice: "Aoede", Language: "de-DE",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	voice := (*captured)["voice"].(map[string]interface{})
	if voice["languageCode"] != "de-DE" || voice["name"] != "de-DE-Chirp3-HD-Aoede" {
		t.Fatalf("voice = %#v", voice)
	}
}

func TestCloudTTS_PassesFullyQualifiedVoiceNameVerbatim(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Bonjour !", Voice: "fr-FR-Chirp3-HD-Charon",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	voice := (*captured)["voice"].(map[string]interface{})
	if voice["languageCode"] != "fr-FR" || voice["name"] != "fr-FR-Chirp3-HD-Charon" {
		t.Fatalf("voice = %#v", voice)
	}
}

func TestCloudTTS_DefaultsLanguageWhenVoiceHasNoLocalePrefix(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello!", Voice: "Chirp3-HD-Kore",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	voice := (*captured)["voice"].(map[string]interface{})
	if voice["languageCode"] != "en-US" || voice["name"] != "Chirp3-HD-Kore" {
		t.Fatalf("voice = %#v", voice)
	}
}

func TestCloudTTS_PrefersExplicitLanguageOverVoiceNameLocale(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello!", Voice: "en-AU-Chirp3-HD-Kore", Language: "en-GB",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	voice := (*captured)["voice"].(map[string]interface{})
	if voice["languageCode"] != "en-GB" || voice["name"] != "en-AU-Chirp3-HD-Kore" {
		t.Fatalf("voice = %#v", voice)
	}
}

func TestCloudTTS_DecodesBase64AudioContent(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!"})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if string(result.Audio) != string(chirpAudioBytes) {
		t.Fatalf("Audio = %v, want %v", result.Audio, chirpAudioBytes)
	}
	googleMeta := result.ProviderMetadata["google"].(map[string]interface{})
	if googleMeta["mimeType"] != "audio/wav" {
		t.Fatalf("providerMetadata.google = %#v", googleMeta)
	}
}

func TestCloudTTS_MapsSpeedToSpeakingRate(t *testing.T) {
	server, captured := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	speed := 1.5
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!", Speed: &speed})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	audioConfig := (*captured)["audioConfig"].(map[string]interface{})
	if audioConfig["speakingRate"] != 1.5 {
		t.Fatalf("audioConfig.speakingRate = %v, want 1.5", audioConfig["speakingRate"])
	}
}

func TestCloudTTS_WarnsAboutUnsupportedInstructions(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello!", Instructions: "Speak slowly.",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Type != "unsupported" || result.Warnings[0].Feature != "instructions" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestCloudTTS_WarnsAboutUnsupportedOutputFormat(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello!", OutputFormat: "mp3",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "outputFormat" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestCloudTTS_NoWarningForWavOutputFormat(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello!", OutputFormat: "wav",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", result.Warnings)
	}
}

func TestCloudTTS_EmptyAudioContentReturnsEmptyAudio(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!"})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Audio) != 0 {
		t.Fatalf("Audio = %v, want empty", result.Audio)
	}
}

func TestCloudTTS_IncludesResponseMetadataAndRequestBody(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!"})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if result.Response == nil || result.Response.ModelID != "chirp-3-hd" {
		t.Fatalf("Response = %+v", result.Response)
	}
	if result.Request == nil {
		t.Fatal("expected Request to be set")
	}
	var reqBody map[string]interface{}
	if err := json.Unmarshal([]byte(result.Request.Body.(string)), &reqBody); err != nil {
		t.Fatalf("Request.Body not valid JSON: %v", err)
	}
	if reqBody["voice"].(map[string]interface{})["name"] != "en-US-Chirp3-HD-Kore" {
		t.Fatalf("request body = %#v", reqBody)
	}
}

func TestCloudTTS_ErrorResponseReturnsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Voice not found.","status":"INVALID_ARGUMENT"}}`))
	}))
	defer server.Close()

	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello!"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestCloudTTS_ExpressModeRejected(t *testing.T) {
	p, err := New(Config{APIKey: "vertex-api-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := p.SpeechModel("chirp-3-hd"); err == nil {
		t.Fatal("expected an Express Mode error for a Chirp speech model")
	}
	// Non-chirp models remain unaffected by Express Mode restrictions here.
	if _, err := p.SpeechModel("gemini-2.5-flash-tts"); err != nil {
		t.Fatalf("SpeechModel(gemini-2.5-flash-tts) error = %v", err)
	}
}

// sanity: base64 fixture matches the raw bytes it decodes to.
func TestCloudTTS_FixtureConsistency(t *testing.T) {
	decoded, err := base64.StdEncoding.DecodeString(chirpAudioBase64)
	if err != nil || string(decoded) != string(chirpAudioBytes) {
		t.Fatalf("fixture mismatch: decoded=%v err=%v", decoded, err)
	}
}
