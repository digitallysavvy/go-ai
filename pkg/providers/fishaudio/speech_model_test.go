package fishaudio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	return p, server
}

// TestSpeechModel_RequiredParameters mirrors "should generate speech with
// required parameters".
func TestSpeechModel_RequiredParameters(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["text"] != "Hello, world!" || seenBody["format"] != "mp3" {
		t.Fatalf("body = %#v", seenBody)
	}
	if _, ok := seenBody["reference_id"]; ok {
		t.Fatalf("reference_id should be omitted: %#v", seenBody)
	}
}

// TestSpeechModel_ModelHeader mirrors "should send the model id as the
// `model` header".
func TestSpeechModel_ModelHeader(t *testing.T) {
	var seenModelHeader string
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenModelHeader = r.Header.Get("model")
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS21Pro)

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenModelHeader != "s2.1-pro" {
		t.Fatalf("model header = %q", seenModelHeader)
	}
}

// TestSpeechModel_BearerAuth mirrors "should pass the api key as a bearer token".
func TestSpeechModel_BearerAuth(t *testing.T) {
	var seenAuth string
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenAuth != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", seenAuth)
	}
}

// TestSpeechModel_VoiceMapsToReferenceID mirrors "should map voice to reference_id".
func TestSpeechModel_VoiceMapsToReferenceID(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Hello, world!",
		Voice: "test-reference-id",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["reference_id"] != "test-reference-id" {
		t.Fatalf("reference_id = %#v", seenBody["reference_id"])
	}
}

// TestSpeechModel_ReferenceIDOverridesVoice mirrors "should let
// providerOptions.referenceId override voice".
func TestSpeechModel_ReferenceIDOverridesVoice(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Hello, world!",
		Voice: "ignored-voice",
		ProviderOptions: map[string]interface{}{
			"fishAudio": SpeechModelOptions{ReferenceID: []string{"speaker-a", "speaker-b"}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	refIDs, ok := seenBody["reference_id"].([]interface{})
	if !ok || len(refIDs) != 2 || refIDs[0] != "speaker-a" || refIDs[1] != "speaker-b" {
		t.Fatalf("reference_id = %#v", seenBody["reference_id"])
	}
}

// TestSpeechModel_OutputFormats mirrors "should map supported output formats"
// and "should warn and fall back to mp3 for an unsupported output format".
func TestSpeechModel_OutputFormats(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	for _, format := range []string{"wav", "pcm", "mp3", "opus"} {
		result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
			Text:         "Hello, world!",
			OutputFormat: format,
		})
		if err != nil {
			t.Fatalf("DoGenerate(%s): %v", format, err)
		}
		if seenBody["format"] != format {
			t.Fatalf("format = %#v, want %q", seenBody["format"], format)
		}
		if len(result.Warnings) != 0 {
			t.Fatalf("unexpected warnings for %s: %#v", format, result.Warnings)
		}
	}

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello, world!",
		OutputFormat: "flac",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["format"] != "mp3" {
		t.Fatalf("fallback format = %#v", seenBody["format"])
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "outputFormat" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_Speed mirrors "should map speed to prosody.speed" and
// "should warn for an out-of-range speed".
func TestSpeechModel_Speed(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	speed := 1.5
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Speed: &speed})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	prosody, ok := seenBody["prosody"].(map[string]interface{})
	if !ok || prosody["speed"] != 1.5 {
		t.Fatalf("prosody = %#v", seenBody["prosody"])
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}

	outOfRange := 3.0
	result, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!", Speed: &outOfRange})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := seenBody["prosody"]; ok {
		t.Fatalf("prosody should be omitted: %#v", seenBody)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "speed" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_NormalizeLoudness mirrors the three normalizeLoudness test
// cases: merged into prosody for s2-pro, sent for s2.1-pro, and dropped with
// a warning for s1.
func TestSpeechModel_NormalizeLoudness(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})

	speed := 1.2
	volume := -3.0
	normalizeFalse := false
	s2 := mustSpeechModel(t, p, ModelS2Pro)
	result, err := s2.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Hello, world!",
		Speed: &speed,
		ProviderOptions: map[string]interface{}{
			"fishAudio": SpeechModelOptions{Volume: &volume, NormalizeLoudness: &normalizeFalse},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	prosody := seenBody["prosody"].(map[string]interface{})
	if prosody["speed"] != 1.2 || prosody["volume"] != -3.0 || prosody["normalize_loudness"] != false {
		t.Fatalf("prosody = %#v", prosody)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}

	normalizeTrue := true
	s21 := mustSpeechModel(t, p, ModelS21Pro)
	result, err = s21.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello, world!",
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{NormalizeLoudness: &normalizeTrue}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	prosody = seenBody["prosody"].(map[string]interface{})
	if prosody["normalize_loudness"] != true {
		t.Fatalf("prosody = %#v", prosody)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}

	s1 := mustSpeechModel(t, p, ModelS1)
	result, err = s1.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello, world!",
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{NormalizeLoudness: &normalizeTrue}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := seenBody["prosody"]; ok {
		t.Fatalf("prosody should be omitted for s1: %#v", seenBody)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.fishAudio.normalizeLoudness" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_LanguageAndInstructionsWarnings mirrors "should warn for
// language and instructions".
func TestSpeechModel_LanguageAndInstructionsWarnings(t *testing.T) {
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello, world!",
		Language:     "en",
		Instructions: "Speak slowly",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 2 || result.Warnings[0].Feature != "language" || result.Warnings[1].Feature != "instructions" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_ProviderOptionsPassthrough mirrors "should pass through
// provider options".
func TestSpeechModel_ProviderOptionsPassthrough(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	sampleRate := 44100
	mp3Bitrate := 192
	temp := 0.5
	topP := 0.9
	chunkLen := 200
	minChunkLen := 20
	normalize := false
	maxNewTokens := 2048
	repetitionPenalty := 1.5
	conditionOnPrevious := false
	earlyStop := 0.8

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello, world!",
		ProviderOptions: map[string]interface{}{
			"fishAudio": SpeechModelOptions{
				SampleRate:                &sampleRate,
				Mp3Bitrate:                &mp3Bitrate,
				Latency:                   "balanced",
				Temperature:               &temp,
				TopP:                      &topP,
				ChunkLength:               &chunkLen,
				MinChunkLength:            &minChunkLen,
				Normalize:                 &normalize,
				MaxNewTokens:              &maxNewTokens,
				RepetitionPenalty:         &repetitionPenalty,
				ConditionOnPreviousChunks: &conditionOnPrevious,
				EarlyStopThreshold:        &earlyStop,
				Features:                  []string{"quality-guard"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	want := map[string]interface{}{
		"text":                         "Hello, world!",
		"format":                       "mp3",
		"sample_rate":                  float64(44100),
		"mp3_bitrate":                  float64(192),
		"latency":                      "balanced",
		"temperature":                  0.5,
		"top_p":                        0.9,
		"chunk_length":                 float64(200),
		"min_chunk_length":             float64(20),
		"normalize":                    false,
		"max_new_tokens":               float64(2048),
		"repetition_penalty":           1.5,
		"condition_on_previous_chunks": false,
		"early_stop_threshold":         0.8,
	}
	for k, v := range want {
		if seenBody[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, seenBody[k], v)
		}
	}
	features, ok := seenBody["features"].([]interface{})
	if !ok || len(features) != 1 || features[0] != "quality-guard" {
		t.Fatalf("features = %#v", seenBody["features"])
	}
}

// TestSpeechModel_BitrateFormatMismatch mirrors "should warn when mp3Bitrate
// is used with a non-mp3 format", "should warn when opusBitrate is used with
// a non-opus format", and "should send opus_bitrate for opus output".
func TestSpeechModel_BitrateFormatMismatch(t *testing.T) {
	var seenBody map[string]interface{}
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel(ModelS1)

	mp3Bitrate := 192
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello, world!",
		OutputFormat:    "opus",
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{Mp3Bitrate: &mp3Bitrate}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := seenBody["mp3_bitrate"]; ok {
		t.Fatalf("mp3_bitrate should be omitted: %#v", seenBody)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.fishAudio.mp3Bitrate" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}

	opusBitrate := 48000
	result, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello, world!",
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{OpusBitrate: &opusBitrate}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := seenBody["opus_bitrate"]; ok {
		t.Fatalf("opus_bitrate should be omitted: %#v", seenBody)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.fishAudio.opusBitrate" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}

	auto := -1000
	result, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "Hello, world!",
		OutputFormat:    "opus",
		ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{OpusBitrate: &auto}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["format"] != "opus" || seenBody["opus_bitrate"] != float64(-1000) {
		t.Fatalf("body = %#v", seenBody)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
}

// TestSpeechModel_ResponseMetadata mirrors "should return the audio and
// response metadata".
func TestSpeechModel_ResponseMetadata(t *testing.T) {
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		w.Header().Set("x-request-id", "test-request-id")
		_, _ = io.WriteString(w, string(make([]byte, 100)))
	})
	model, _ := p.SpeechModel(ModelS1)

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Audio) != 100 {
		t.Fatalf("audio length = %d", len(result.Audio))
	}
	if result.Response.ModelID != ModelS1 {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
	if result.Request.Body != `{"format":"mp3","text":"Hello, world!"}` {
		// map key order from json.Marshal is alphabetical for map[string]interface{}.
		t.Fatalf("request body = %q", result.Request.Body)
	}
}

// TestSpeechModel_APIErrors mirrors "should surface API errors".
func TestSpeechModel_APIErrors(t *testing.T) {
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"status":402,"message":"No payment -- see charging schemes"}`))
	})
	model, _ := p.SpeechModel(ModelS1)

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello, world!"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "No payment -- see charging schemes") {
		t.Fatalf("error = %q", got)
	}
}

func mustSpeechModel(t *testing.T, p *Provider, modelID string) provider.SpeechModel {
	t.Helper()
	m, err := p.SpeechModel(modelID)
	if err != nil {
		t.Fatalf("SpeechModel(%s): %v", modelID, err)
	}
	return m
}
