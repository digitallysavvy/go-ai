package cartesia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newCartesiaTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{APIKey: "test-api-key", BaseURL: server.URL})
}

// TestSpeechModel_RequiredParameters mirrors "should generate speech with
// required parameters".
func TestSpeechModel_RequiredParameters(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "test-voice-id"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	want := map[string]interface{}{
		"model_id":   "sonic-3.5",
		"transcript": "Hello, world!",
		"voice":      map[string]interface{}{"mode": "id", "id": "test-voice-id"},
		"output_format": map[string]interface{}{
			"container":   "mp3",
			"sample_rate": float64(44100),
			"bit_rate":    float64(128000),
		},
	}
	if seenBody["model_id"] != want["model_id"] || seenBody["transcript"] != want["transcript"] {
		t.Fatalf("body = %#v", seenBody)
	}
	if outputFormat, ok := seenBody["output_format"].(map[string]interface{}); !ok || outputFormat["container"] != "mp3" || outputFormat["sample_rate"] != float64(44100) || outputFormat["bit_rate"] != float64(128000) {
		t.Fatalf("output_format = %#v", seenBody["output_format"])
	}
}

// TestSpeechModel_RequiresVoice mirrors "should throw when no voice is provided".
func TestSpeechModel_RequiresVoice(t *testing.T) {
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"})
	if err == nil {
		t.Fatal("expected error for missing voice")
	}
}

// TestSpeechModel_OutputFormats mirrors "should map wav output format" and
// "should map pcm output format with sample rate suffix".
func TestSpeechModel_OutputFormats(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", OutputFormat: "wav"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	of := seenBody["output_format"].(map[string]interface{})
	if of["container"] != "wav" || of["encoding"] != "pcm_s16le" || of["sample_rate"] != float64(44100) {
		t.Fatalf("wav output_format = %#v", of)
	}

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", OutputFormat: "pcm_24000"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	of = seenBody["output_format"].(map[string]interface{})
	if of["container"] != "raw" || of["encoding"] != "pcm_f32le" || of["sample_rate"] != float64(24000) {
		t.Fatalf("pcm_24000 output_format = %#v", of)
	}
}

// TestSpeechModel_LanguageAndSpeed mirrors "should handle language
// parameter" and "should handle speed parameter".
func TestSpeechModel_LanguageAndSpeed(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hola, mundo!", Voice: "v", Language: "es"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["transcript"] != "Hola, mundo!" || seenBody["language"] != "es" {
		t.Fatalf("body = %#v", seenBody)
	}

	speed := 1.5
	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", Speed: &speed}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	genConfig := seenBody["generation_config"].(map[string]interface{})
	if genConfig["speed"] != 1.5 {
		t.Fatalf("generation_config = %#v", genConfig)
	}
}

// TestSpeechModel_SpeedOutOfRangeWarning mirrors "should warn and ignore an
// out-of-range generic speed".
func TestSpeechModel_SpeedOutOfRangeWarning(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	speed := 2.0
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", Speed: &speed})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := seenBody["generation_config"]; ok {
		t.Fatalf("generation_config should be omitted: %#v", seenBody)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "speed" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_InstructionsWarning mirrors "should warn about unsupported
// instructions parameter".
func TestSpeechModel_InstructionsWarning(t *testing.T) {
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", Instructions: "Speak slowly"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "instructions" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_ProviderSpecificOptions mirrors "should pass
// provider-specific options" and "should ignore encoding for mp3 output".
func TestSpeechModel_ProviderSpecificOptions(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	sampleRate := 16000
	speed := 0.8
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, world!", Voice: "v",
		ProviderOptions: map[string]interface{}{
			"cartesia": SpeechModelOptions{Container: "raw", Encoding: "pcm_s16le", SampleRate: &sampleRate, Speed: &speed},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	of := seenBody["output_format"].(map[string]interface{})
	if of["container"] != "raw" || of["encoding"] != "pcm_s16le" || of["sample_rate"] != float64(16000) {
		t.Fatalf("output_format = %#v", of)
	}
	genConfig := seenBody["generation_config"].(map[string]interface{})
	if genConfig["speed"] != 0.8 {
		t.Fatalf("generation_config = %#v", genConfig)
	}

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, world!", Voice: "v",
		ProviderOptions: map[string]interface{}{"cartesia": SpeechModelOptions{Encoding: "pcm_s16le"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	of = seenBody["output_format"].(map[string]interface{})
	if of["container"] != "mp3" || of["sample_rate"] != float64(44100) || of["bit_rate"] != float64(128000) {
		t.Fatalf("output_format = %#v", of)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.cartesia.encoding" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_UnsupportedSampleRateSuffix mirrors "should warn about an
// unsupported sample rate suffix".
func TestSpeechModel_UnsupportedSampleRateSuffix(t *testing.T) {
	var seenBody map[string]interface{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v", OutputFormat: "wav_12345"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	of := seenBody["output_format"].(map[string]interface{})
	if of["container"] != "wav" || of["sample_rate"] != float64(44100) {
		t.Fatalf("output_format = %#v", of)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "outputFormat" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_Headers mirrors "should pass headers".
func TestSpeechModel_Headers(t *testing.T) {
	var seenHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL, APIVersion: "2026-03-01"})
	model, _ := p.SpeechModel(ModelSonic35)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, world!", Voice: "v",
		Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenHeaders.Get("Authorization") != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", seenHeaders.Get("Authorization"))
	}
	if seenHeaders.Get("Cartesia-Version") != "2026-03-01" {
		t.Fatalf("cartesia-version = %q", seenHeaders.Get("Cartesia-Version"))
	}
	if seenHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Fatalf("custom header = %q", seenHeaders.Get("Custom-Request-Header"))
	}
}

// TestSpeechModel_ResponseMetadata mirrors "should include response data
// with timestamp, modelId and headers".
func TestSpeechModel_ResponseMetadata(t *testing.T) {
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		w.Header().Set("x-request-id", "test-request-id")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelSonic35)

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Voice: "v"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Audio) != 100 {
		t.Fatalf("audio length = %d", len(result.Audio))
	}
	if result.Response.ModelID != ModelSonic35 {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}
