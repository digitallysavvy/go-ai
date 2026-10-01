package assemblyai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderUnsupportedModels(t *testing.T) {
	p := New(Config{APIKey: "k"})

	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel should return unsupported error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel should return unsupported error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel should return unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel should return unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel should return unsupported error")
	}
}

func TestDoTranscribe_LegacyBestModel(t *testing.T) {
	t.Parallel()

	var (
		seenUploadAuth    string
		seenTranscriptRaw map[string]interface{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			seenUploadAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"upload_url": "https://files.example/audio",
			})
			return
		case "/transcript":
			_ = json.NewDecoder(r.Body).Decode(&seenTranscriptRaw)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     "tx-1",
				"status": "queued",
			})
			return
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             "tx-1",
				"status":         "completed",
				"text":           "hello world",
				"audio_duration": 2.3,
				"words": []map[string]interface{}{
					{"text": "hello", "start": 0, "end": 1000},
					{"text": "world", "start": 1100, "end": 2300},
				},
			})
			return
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "asm-key", BaseURL: srv.URL})
	mAny, err := p.TranscriptionModel("best")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	m := mAny.(*TranscriptionModel)
	if m.ModelID() != "best" {
		t.Fatalf("ModelID = %q, want best", m.ModelID())
	}

	got, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("abc"),
		Language: "en",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	if seenUploadAuth != "asm-key" {
		t.Fatalf("Authorization header = %q, want asm-key", seenUploadAuth)
	}
	if seenTranscriptRaw["audio_url"] != "https://files.example/audio" {
		t.Fatalf("audio_url = %#v", seenTranscriptRaw["audio_url"])
	}
	if seenTranscriptRaw["language_code"] != "en" {
		t.Fatalf("language_code = %#v", seenTranscriptRaw["language_code"])
	}
	if seenTranscriptRaw["speech_model"] != "best" {
		t.Fatalf("speech_model = %#v", seenTranscriptRaw["speech_model"])
	}
	if _, ok := seenTranscriptRaw["speech_models"]; ok {
		t.Fatalf("speech_models should not be set for 'best' model")
	}
	if got.Text != "hello world" {
		t.Fatalf("Text = %q", got.Text)
	}
	if len(got.Timestamps) != 2 {
		t.Fatalf("timestamps len = %d, want 2", len(got.Timestamps))
	}
	// Word timings are in ms and must be converted to seconds.
	if got.Timestamps[0].Start != 0 || got.Timestamps[0].End != 1 {
		t.Fatalf("timestamps[0] = %#v", got.Timestamps[0])
	}
	if got.Usage.DurationSeconds != 2.3 {
		t.Fatalf("duration = %v, want 2.3", got.Usage.DurationSeconds)
	}
	foundDeprecation := false
	for _, w := range got.Warnings {
		if w.Type == "deprecated" {
			foundDeprecation = true
			if w.Setting != "model 'best'" {
				t.Fatalf("deprecated warning setting = %q", w.Setting)
			}
			if !strings.Contains(w.Message, "universal-3-5-pro") {
				t.Fatalf("deprecated warning message = %q", w.Message)
			}
		}
	}
	if !foundDeprecation {
		t.Fatal("expected a deprecation warning for the 'best' model")
	}
}

func TestDoTranscribe_NewerModelsUseSpeechModelsArray(t *testing.T) {
	t.Parallel()

	var seenTranscriptRaw map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "https://files.example/audio"})
		case "/transcript":
			_ = json.NewDecoder(r.Body).Decode(&seenTranscriptRaw)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "queued"})
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "tx-1", "status": "completed", "text": "hi",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	cases := []struct {
		model        string
		wantWarnType string
		wantContains string
	}{
		{ModelUniversal35Pro, "", ""},
		{ModelUniversal3Pro, "other", "replace 'universal-3-pro'"},
		{ModelUniversal2, "other", "universal-3-5-pro"},
		{"nano", "", ""},
	}

	for _, tc := range cases {
		mAny, err := p.TranscriptionModel(tc.model)
		if err != nil {
			t.Fatalf("TranscriptionModel(%s): %v", tc.model, err)
		}
		result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
		if err != nil {
			t.Fatalf("DoTranscribe(%s): %v", tc.model, err)
		}

		models, ok := seenTranscriptRaw["speech_models"].([]interface{})
		if !ok || len(models) != 1 || models[0] != tc.model {
			t.Fatalf("speech_models for %s = %#v", tc.model, seenTranscriptRaw["speech_models"])
		}
		if _, ok := seenTranscriptRaw["speech_model"]; ok {
			t.Fatalf("speech_model should not be set for %s", tc.model)
		}

		if tc.wantWarnType == "" {
			for _, w := range result.Warnings {
				if w.Type == "deprecated" || w.Type == "other" {
					t.Fatalf("model %s: unexpected warning %#v", tc.model, w)
				}
			}
			continue
		}
		found := false
		for _, w := range result.Warnings {
			if w.Type == tc.wantWarnType && strings.Contains(w.Message, tc.wantContains) {
				found = true
			}
		}
		if !found {
			t.Fatalf("model %s: expected warning containing %q, got %#v", tc.model, tc.wantContains, result.Warnings)
		}
	}
}

func TestDoTranscribe_ProviderOptions(t *testing.T) {
	t.Parallel()

	var seenTranscriptRaw map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "https://files.example/audio"})
		case "/transcript":
			_ = json.NewDecoder(r.Body).Decode(&seenTranscriptRaw)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "queued"})
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "completed", "text": "hi"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)

	_, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"),
		ProviderOptions: map[string]interface{}{
			"assemblyai": map[string]interface{}{
				"prompt":          "This is a conversation about the AI SDK.",
				"keytermsPrompt":  []string{"Vercel", "AI SDK"},
				"temperature":     0.2,
				"removeAudioTags": "speaker",
				"domain":          "medical-v1",
				"speakerOptions": map[string]interface{}{
					"minSpeakersExpected": 1,
					"maxSpeakersExpected": 3,
				},
				"languageDetectionOptions": map[string]interface{}{
					"expectedLanguages":                []string{"en", "es"},
					"fallbackLanguage":                 "en",
					"codeSwitching":                    true,
					"codeSwitchingConfidenceThreshold": 0.5,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	if seenTranscriptRaw["prompt"] != "This is a conversation about the AI SDK." {
		t.Fatalf("prompt = %#v", seenTranscriptRaw["prompt"])
	}
	if seenTranscriptRaw["temperature"] != 0.2 {
		t.Fatalf("temperature = %#v", seenTranscriptRaw["temperature"])
	}
	if seenTranscriptRaw["remove_audio_tags"] != "speaker" {
		t.Fatalf("remove_audio_tags = %#v", seenTranscriptRaw["remove_audio_tags"])
	}
	if seenTranscriptRaw["domain"] != "medical-v1" {
		t.Fatalf("domain = %#v", seenTranscriptRaw["domain"])
	}
	speakerOptions, ok := seenTranscriptRaw["speaker_options"].(map[string]interface{})
	if !ok || speakerOptions["min_speakers_expected"] != float64(1) || speakerOptions["max_speakers_expected"] != float64(3) {
		t.Fatalf("speaker_options = %#v", seenTranscriptRaw["speaker_options"])
	}
	ldo, ok := seenTranscriptRaw["language_detection_options"].(map[string]interface{})
	if !ok || ldo["fallback_language"] != "en" || ldo["code_switching"] != true {
		t.Fatalf("language_detection_options = %#v", seenTranscriptRaw["language_detection_options"])
	}
}

func TestDoTranscribe_DeprecatedBoostWarning(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
		case "/transcript":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "queued"})
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "completed", "text": "hi"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)

	result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"),
		ProviderOptions: map[string]interface{}{
			"assemblyai": map[string]interface{}{
				"wordBoost":  []string{"Vercel"},
				"boostParam": "high",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "deprecated" && w.Setting == "wordBoost, boostParam" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deprecated wordBoost/boostParam warning, got %#v", result.Warnings)
	}
}

func TestDoTranscribe_PrerequisiteWarnings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
		case "/transcript":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "queued"})
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "completed", "text": "hi"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	t.Run("redactStaticEntities without redactPii", func(t *testing.T) {
		mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)
		result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("a"),
			ProviderOptions: map[string]interface{}{
				"assemblyai": map[string]interface{}{
					"redactStaticEntities": map[string][]string{"TOOL": {"Vercel"}},
				},
			},
		})
		if err != nil {
			t.Fatalf("DoTranscribe: %v", err)
		}
		if !containsWarning(result.Warnings, "other", "redactPii") {
			t.Fatalf("expected redactPii warning, got %#v", result.Warnings)
		}
	})

	t.Run("redactPiiAudioOptions without redactPiiAudio", func(t *testing.T) {
		mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)
		result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("a"),
			ProviderOptions: map[string]interface{}{
				"assemblyai": map[string]interface{}{
					"redactPii": true,
					"redactPiiAudioOptions": map[string]interface{}{
						"overrideAudioRedactionMethod": "silence",
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("DoTranscribe: %v", err)
		}
		if !containsWarning(result.Warnings, "other", "redactPiiAudio") {
			t.Fatalf("expected redactPiiAudio warning, got %#v", result.Warnings)
		}
	})

	t.Run("languageCode with languageDetection", func(t *testing.T) {
		mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)
		result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("a"),
			ProviderOptions: map[string]interface{}{
				"assemblyai": map[string]interface{}{
					"languageCode":      "en",
					"languageDetection": true,
				},
			},
		})
		if err != nil {
			t.Fatalf("DoTranscribe: %v", err)
		}
		if !containsWarning(result.Warnings, "other", "languageDetection") {
			t.Fatalf("expected languageDetection warning, got %#v", result.Warnings)
		}
	})
}

func containsWarning(warnings []types.Warning, warnType, substr string) bool {
	for _, w := range warnings {
		if w.Type == warnType && strings.Contains(w.Message, substr) {
			return true
		}
	}
	return false
}

func TestDoTranscribe_ProviderMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
		case "/transcript":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-1", "status": "queued"})
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     "tx-1",
				"status": "completed",
				"text":   "Hello, world!",
				"utterances": []map[string]interface{}{
					{"speaker": "A", "text": "Hello, world!", "start": 250, "end": 26950},
				},
				"entities": []map[string]interface{}{
					{"entity_type": "location", "text": "Canada", "start": 2548, "end": 3130},
				},
				"sentiment_analysis_results": []map[string]interface{}{
					{"text": "Hello, world!", "sentiment": "POSITIVE"},
				},
				"content_safety_labels":  map[string]interface{}{"status": "success"},
				"iab_categories_result":  map[string]interface{}{"status": "success"},
				"auto_highlights_result": map[string]interface{}{"status": "success"},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel(ModelUniversal35Pro)
	result, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	metadata, ok := result.ProviderMetadata["assemblyai"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected assemblyai metadata, got %#v", result.ProviderMetadata)
	}
	if metadata["utterances"] == nil || metadata["entities"] == nil || metadata["sentimentAnalysisResults"] == nil {
		t.Fatalf("metadata missing fields: %#v", metadata)
	}
	if metadata["contentSafetyLabels"] == nil || metadata["iabCategoriesResult"] == nil || metadata["autoHighlightsResult"] == nil {
		t.Fatalf("metadata missing audio-intelligence fields: %#v", metadata)
	}
}

func TestDoTranscribe_PollError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
		case "/transcript":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-2", "status": "queued"})
		case "/transcript/tx-2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-2", "status": "error", "error": "bad audio"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel("")
	_, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
	if err == nil || !strings.Contains(err.Error(), "transcription failed: bad audio") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoTranscribe_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "bad request", "code": 4001},
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel("best")
	_, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("unexpected error: %v", err)
	}
}
