package fishaudio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

var fishAudioTestAudio = []byte{0, 1, 2, 3, 4}

var fishAudioTranscriptionFixture = map[string]interface{}{
	"language":      "English",
	"language_code": "en",
	"text":          "Hello, world!",
	"duration":      2.5,
	"segments": []map[string]interface{}{
		{"text": "Hello,", "start": 0.0, "end": 1.2},
		{"text": "world!", "start": 1.2, "end": 2.5},
	},
}

func jsonHandler(t *testing.T, body interface{}, capture func(r *http.Request)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

// TestTranscriptionModel_MultipartUpload mirrors "should send Uint8Array
// audio as a multipart file named `audio`".
func TestTranscriptionModel_MultipartUpload(t *testing.T) {
	var seenMediaType string
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/asr" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, fh, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		seenMediaType = fh.Header.Get("Content-Type")
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    fishAudioTestAudio,
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenMediaType != "audio/mpeg" {
		t.Fatalf("media type = %q", seenMediaType)
	}
}

// TestTranscriptionModel_Base64Audio mirrors "should accept base64 audio".
func TestTranscriptionModel_Base64Audio(t *testing.T) {
	var seenBytes []byte
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		defer file.Close()
		buf := make([]byte, 16)
		n, _ := file.Read(buf)
		seenBytes = buf[:n]
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	base64Audio := "AAECAwQ=" // base64 of {0,1,2,3,4}
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: base64Audio,
		MimeType:    "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if string(seenBytes) != string(fishAudioTestAudio) {
		t.Fatalf("decoded audio = %v, want %v", seenBytes, fishAudioTestAudio)
	}
}

// TestTranscriptionModel_BearerAuth mirrors "should pass the api key as a
// bearer token".
func TestTranscriptionModel_BearerAuth(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, func(r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenAuth != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", seenAuth)
	}
}

// TestTranscriptionModel_TimestampsDefault mirrors "should request
// timestamps by default" and "should allow opting out of timestamps".
func TestTranscriptionModel_TimestampsDefault(t *testing.T) {
	var seenValue string
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		seenValue = r.FormValue("ignore_timestamps")
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenValue != "false" {
		t.Fatalf("ignore_timestamps = %q", seenValue)
	}

	ignore := true
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           fishAudioTestAudio,
		MimeType:        "audio/mpeg",
		ProviderOptions: map[string]interface{}{"fishAudio": TranscriptionModelOptions{IgnoreTimestamps: &ignore}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenValue != "true" {
		t.Fatalf("ignore_timestamps = %q", seenValue)
	}
}

// TestTranscriptionModel_Language mirrors "should send the language when
// provided" and "should omit the language when not provided".
func TestTranscriptionModel_Language(t *testing.T) {
	var seenHasLanguage bool
	var seenLanguage string
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		seenLanguage = r.FormValue("language")
		_, seenHasLanguage = r.MultipartForm.Value["language"]
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           fishAudioTestAudio,
		MimeType:        "audio/mpeg",
		ProviderOptions: map[string]interface{}{"fishAudio": TranscriptionModelOptions{Language: "en"}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if !seenHasLanguage || seenLanguage != "en" {
		t.Fatalf("language = %q present=%v", seenLanguage, seenHasLanguage)
	}

	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenHasLanguage {
		t.Fatalf("language should be omitted, present=%v", seenHasLanguage)
	}
}

// TestTranscriptionModel_MapsTranscriptSegmentsAndMetadata mirrors "should
// map the transcript, segments and duration", "should report the detected
// language code", "should prefer the detected language over the requested
// one", and "should expose the human-readable language as provider metadata".
func TestTranscriptionModel_MapsTranscriptSegmentsAndMetadata(t *testing.T) {
	server := httptest.NewServer(jsonHandler(t, fishAudioTranscriptionFixture, nil))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           fishAudioTestAudio,
		MimeType:        "audio/mpeg",
		ProviderOptions: map[string]interface{}{"fishAudio": TranscriptionModelOptions{Language: "ja"}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Text != "Hello, world!" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 2.5 {
		t.Fatalf("duration = %#v", result.DurationInSeconds)
	}
	if len(result.Segments) != 2 || result.Segments[0].Text != "Hello," || result.Segments[0].Start != 0 || result.Segments[0].End != 1.2 {
		t.Fatalf("segments = %#v", result.Segments)
	}
	// Fish Audio ignores the requested language when detecting.
	if result.Language != "en" {
		t.Fatalf("language = %q", result.Language)
	}
	meta, ok := result.ProviderMetadata["fishAudio"].(map[string]interface{})
	if !ok || meta["language"] != "English" {
		t.Fatalf("providerMetadata = %#v", result.ProviderMetadata)
	}
}

// TestTranscriptionModel_MissingLanguage mirrors "should report an undefined
// language when the response omits it" and "should fall back to empty
// segments when the response omits them" and "should tolerate a missing
// duration".
func TestTranscriptionModel_MissingLanguage(t *testing.T) {
	body := map[string]interface{}{"text": "Hello, world!", "duration": 1.0}
	server := httptest.NewServer(jsonHandler(t, body, nil))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Language != "" {
		t.Fatalf("language = %q", result.Language)
	}
	if result.ProviderMetadata != nil {
		t.Fatalf("providerMetadata = %#v", result.ProviderMetadata)
	}
	if len(result.Segments) != 0 {
		t.Fatalf("segments = %#v", result.Segments)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 1 {
		t.Fatalf("duration = %#v", result.DurationInSeconds)
	}

	bodyNoDuration := map[string]interface{}{"text": "Hello, world!", "segments": []interface{}{}}
	server2 := httptest.NewServer(jsonHandler(t, bodyNoDuration, nil))
	defer server2.Close()
	p2 := New(Config{APIKey: "test-api-key", BaseURL: server2.URL})
	model2, _ := p2.TranscriptionModel("")
	result2, err := model2.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result2.DurationInSeconds != nil {
		t.Fatalf("duration = %#v", result2.DurationInSeconds)
	}
}

// TestTranscriptionModel_ResponseMetadata mirrors "should return response
// metadata".
func TestTranscriptionModel_ResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "test-request-id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fishAudioTranscriptionFixture)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelTranscribe1)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Response.ModelID != ModelTranscribe1 {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}

// TestTranscriptionModel_APIErrors mirrors "should surface API errors".
func TestTranscriptionModel_APIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"message":"No permission -- see authorization schemes"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("")

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: fishAudioTestAudio, MimeType: "audio/mpeg"})
	if err == nil || !strings.Contains(err.Error(), "No permission -- see authorization schemes") {
		t.Fatalf("error = %v", err)
	}
}
