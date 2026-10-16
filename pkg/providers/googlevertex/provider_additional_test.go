package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestVertexProvider_CreateAliasesAndClient(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	}
	p1, err := CreateGoogleVertex(cfg)
	if err != nil {
		t.Fatalf("CreateGoogleVertex() error = %v", err)
	}
	if p1 == nil || p1.Client() == nil {
		t.Fatal("CreateGoogleVertex returned nil provider or nil client")
	}

	p2, err := CreateVertex(cfg)
	if err != nil {
		t.Fatalf("CreateVertex() error = %v", err)
	}
	if p2 == nil || p2.Client() == nil {
		t.Fatal("CreateVertex returned nil provider or nil client")
	}
}

func TestVertexProviderEnvironmentFallbacks(t *testing.T) {
	const envKey = "env-vertex-key"
	const envProject = "env-project"
	const envLocation = "env-location"
	origKey := os.Getenv("GOOGLE_VERTEX_API_KEY")
	origProject := os.Getenv("GOOGLE_VERTEX_PROJECT")
	origLocation := os.Getenv("GOOGLE_VERTEX_LOCATION")
	t.Cleanup(func() {
		if origKey == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_API_KEY")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_API_KEY", origKey)
		}
		if origProject == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_PROJECT")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_PROJECT", origProject)
		}
		if origLocation == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_LOCATION")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_LOCATION", origLocation)
		}
	})
	if err := os.Setenv("GOOGLE_VERTEX_API_KEY", envKey); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_API_KEY failed: %v", err)
	}
	if err := os.Setenv("GOOGLE_VERTEX_PROJECT", envProject); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_PROJECT failed: %v", err)
	}
	if err := os.Setenv("GOOGLE_VERTEX_LOCATION", envLocation); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_LOCATION failed: %v", err)
	}

	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// The API key is read when the provider is created; project and location
	// are read lazily (TS loadSetting inside loadBaseURL).
	if p.config.APIKey != envKey || p.config.Project != "" || p.config.Location != "" {
		t.Fatalf("config = %#v", p.config)
	}
	if p.Project() != envProject || p.Location() != envLocation {
		t.Fatalf("Project/Location = %q/%q", p.Project(), p.Location())
	}
}

func TestVertexHostMatchesTypeScriptProvider(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"global":      "aiplatform.googleapis.com",
		"eu":          "aiplatform.eu.rep.googleapis.com",
		"us":          "aiplatform.us.rep.googleapis.com",
		"us-central1": "us-central1-aiplatform.googleapis.com",
	}
	for location, want := range tests {
		if got := vertexHost(location); got != want {
			t.Fatalf("vertexHost(%q) = %q, want %q", location, got, want)
		}
	}
}

func TestVertexChirpTranscriptionRequestAndResponse(t *testing.T) {
	var capturedURL string
	var capturedBody map[string]interface{}
	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			capturedURL = r.URL.String()
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("Authorization = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			return jsonResponse(200, `{
				"results":[{
					"alternatives":[{
						"transcript":"hello world",
						"words":[
							{"word":"hello","startOffset":"0s","endOffset":"0.500s"},
							{"word":"world","startOffset":"0.500s","endOffset":"1s"}
						]
					}],
					"languageCode":"en-US"
				}],
				"metadata":{"totalBilledDuration":"1s"}
			}`), nil
		})},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("chirp_2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: "AQIDBAUGBwg=",
		MimeType:    "audio/wav",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"languageCodes": []interface{}{"en-US", "fr-FR"}},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if capturedURL != "https://us-central1-speech.googleapis.com/v2/projects/test-project/locations/us-central1/recognizers/_:recognize" {
		t.Fatalf("url = %q", capturedURL)
	}
	config := capturedBody["config"].(map[string]interface{})
	if config["model"] != "chirp_2" || capturedBody["content"] != "AQIDBAUGBwg=" {
		t.Fatalf("body = %#v", capturedBody)
	}
	if result.Text != "hello world" || result.Language != "en" || result.DurationInSeconds == nil || *result.DurationInSeconds != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Segments) != 2 || result.Segments[1].End != 1 {
		t.Fatalf("segments = %#v", result.Segments)
	}
	if result.Response == nil || result.Response.Timestamp.IsZero() || result.Response.ModelID != "chirp_2" {
		t.Fatalf("response metadata = %#v", result.Response)
	}
	if result.Response.Headers["Content-Type"] != "application/json" {
		t.Fatalf("response headers = %#v", result.Response.Headers)
	}
	body, ok := result.Response.Body.(map[string]interface{})
	if !ok || body["metadata"] == nil {
		t.Fatalf("response body = %#v, want decoded raw response", result.Response.Body)
	}
}

// TestVertexTranscriptionRejectsRegionThatWouldRewriteHost ports TS
// google-vertex-transcription-model.test.ts's region validation (TS
// #21842): region (from providerOptions.googleVertex.region, falling back
// to the provider's location) is interpolated directly into the Cloud
// Speech-to-Text request host, so a value that isn't a single DNS label
// must be rejected with no request sent.
func TestVertexTranscriptionRejectsRegionThatWouldRewriteHost(t *testing.T) {
	calls := 0
	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return jsonResponse(200, `{}`), nil
		})},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("chirp_2")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	_, err = model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: "AQIDBAUGBwg=",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"region": "user@internal:8080/#"},
		},
	})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("DoTranscribe error = %v, want InvalidArgumentError", err)
	}
	if invalid.Field != "region" {
		t.Fatalf("Field = %q, want region", invalid.Field)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0 (no request sent)", calls)
	}
}

// TestVertexTranscriptionRejectsInvalidLocationFallback ports TS
// google-vertex-location-validation.test.ts: provider.transcription() validates
// the location for the separate transcription host, even with a custom baseURL.
func TestVertexTranscriptionRejectsInvalidLocationFallback(t *testing.T) {
	p, err := New(Config{
		Project:     "test-project",
		Location:    "user@internal:8080/#",
		BaseURL:     "https://proxy.example/v1beta1/projects/test-project/locations/x/publishers/google",
		AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, id := range []string{"chirp_2", "gemini-3.5-transcribe-live"} {
		_, err = p.TranscriptionModel(id)
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "location" {
			t.Fatalf("TranscriptionModel(%s) error = %v, want InvalidArgumentError for location", id, err)
		}
	}
}

// TS: a custom baseURL needs neither project nor location (loadBaseURL returns
// it before loading either); transcription models still need both.
func TestVertexCustomBaseURLNeedsNoProjectOrLocation(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "")
	p, err := New(Config{BaseURL: "https://proxy.example/v1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.LanguageModel("gemini-2.5-flash"); err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if _, err := p.TranscriptionModel("chirp_2"); err == nil || !strings.Contains(err.Error(), "project setting is missing") {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	// Without a baseURL the settings are needed, but only when a model is
	// created (TS loads location first, then project).
	p2, err := New(Config{AccessToken: "token"})
	if err != nil {
		t.Fatalf("New without baseURL: %v", err)
	}
	if _, err := p2.LanguageModel("gemini-2.5-flash"); err == nil || !strings.Contains(err.Error(), "location setting is missing") {
		t.Fatalf("LanguageModel without baseURL error = %v", err)
	}
}

// TS: the generated bearer token comes first and the caller's headers are
// spread after it, so a caller-supplied Authorization header wins; in express
// mode the API key is set last and wins over a same-named header.
func TestVertexCallerAuthorizationHeaderWinsAndAPIKeyWins(t *testing.T) {
	var got http.Header
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Clone()
		return jsonResponse(200, `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`), nil
	})
	call := func(cfg Config) http.Header {
		cfg.HTTPClient = &http.Client{Transport: rt}
		p, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.LanguageModel("gemini-2.5-flash")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return got
	}
	h := call(Config{Project: "p", Location: "us-central1", AccessToken: "generated", Headers: map[string]string{"Authorization": "Bearer mine"}})
	if h.Get("Authorization") != "Bearer mine" {
		t.Fatalf("Authorization = %q", h.Get("Authorization"))
	}
	h = call(Config{Project: "p", Location: "us-central1", AccessToken: "generated"})
	if h.Get("Authorization") != "Bearer generated" {
		t.Fatalf("Authorization = %q", h.Get("Authorization"))
	}
	h = call(Config{APIKey: "key", Headers: map[string]string{"X-Goog-Api-Key": "other"}})
	if h.Get("X-Goog-Api-Key") != "key" {
		t.Fatalf("x-goog-api-key = %q", h.Get("X-Goog-Api-Key"))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestVertexProvider_ModelFactoriesAndUnsupportedMethods(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	em, err := p.EmbeddingModel("text-embedding-005")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if em.Provider() != "google.vertex.embedding" {
		t.Fatalf("EmbeddingModel provider = %q, want google-vertex", em.Provider())
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("EmbeddingModel(\"\") expected error")
	}

	speech, err := p.SpeechModel("gemini-2.5-flash-tts")
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	if speech.Provider() != "google.vertex.speech" || speech.ModelID() != "gemini-2.5-flash-tts" {
		t.Fatalf("SpeechModel metadata = %s/%s", speech.Provider(), speech.ModelID())
	}
	emptySpeech, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel(\"\") should preserve the caller model ID, got error %v", err)
	}
	if emptySpeech.ModelID() != "" {
		t.Fatalf("SpeechModel(\"\").ModelID() = %q, want empty string", emptySpeech.ModelID())
	}
	transcription, err := p.TranscriptionModel("chirp_2")
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	if transcription.Provider() != "google.vertex.transcription" || transcription.ModelID() != "chirp_2" {
		t.Fatalf("TranscriptionModel metadata = %s/%s", transcription.Provider(), transcription.ModelID())
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
	if _, err := p.VideoModel(""); err == nil {
		t.Fatal("VideoModel(\"\") expected validation error")
	}
	video, err := p.VideoModel("veo-3.0-generate-preview")
	if err != nil {
		t.Fatalf("VideoModel() error = %v", err)
	}
	if video.Provider() != "google.vertex.video" || video.ModelID() != "veo-3.0-generate-preview" {
		t.Fatalf("VideoModel metadata = %s/%s", video.Provider(), video.ModelID())
	}
}
