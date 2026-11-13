package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func transcriptionTestServer(t *testing.T, capture *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/speech-to-text" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if capture != nil {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Fatalf("ParseMultipartForm: %v", err)
			}
			for k, v := range r.MultipartForm.Value {
				(*capture)[k] = v[0]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"language_code":        "en",
			"language_probability": 0.99,
			"text":                 "Hello from the Go AI SDK.",
			"words": []map[string]interface{}{
				{"text": "Hello", "type": "word", "start": 0.0, "end": 0.5},
				{"text": "from", "type": "word", "start": 0.5, "end": 1.0},
			},
		})
	}))
}

func TestElevenLabsTranscription_RejectsRealtimeModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m, _ := p.TranscriptionModel(ModelScribeV2Realtime)
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || !errors.Is(err, providererrors.ErrUnsupportedFeature) {
		t.Fatalf("expected UnsupportedFunctionalityError, got %v", err)
	}
}

func TestElevenLabsTranscription_PassesModel(t *testing.T) {
	capture := map[string]string{}
	srv := transcriptionTestServer(t, &capture)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if capture["model_id"] != "scribe_v1" {
		t.Fatalf("model_id = %q", capture["model_id"])
	}
	// No providerOptions: default diarize should be "true".
	if capture["diarize"] != "true" {
		t.Fatalf("diarize = %q", capture["diarize"])
	}
	if result.Text != "Hello from the Go AI SDK." {
		t.Fatalf("Text = %q", result.Text)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("segments = %#v", result.Segments)
	}
}

func TestElevenLabsTranscription_ProviderOptions(t *testing.T) {
	capture := map[string]string{}
	srv := transcriptionTestServer(t, &capture)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"languageCode":          "en",
				"fileFormat":            "pcm_s16le_16",
				"tagAudioEvents":        false,
				"numSpeakers":           2,
				"timestampsGranularity": "character",
				"diarize":               true,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	want := map[string]string{
		"diarize":                "true",
		"file_format":            "pcm_s16le_16",
		"language_code":          "en",
		"model_id":               "scribe_v1",
		"num_speakers":           "2",
		"tag_audio_events":       "false",
		"timestamps_granularity": "character",
	}
	for k, v := range want {
		if capture[k] != v {
			t.Fatalf("field %s = %q, want %q (all: %#v)", k, capture[k], v, capture)
		}
	}
}

func TestElevenLabsTranscription_WarnsOnStreamingOptions(t *testing.T) {
	srv := transcriptionTestServer(t, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"includeTimestamps": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.elevenlabs.streaming" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestElevenLabsTranscription_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "bad audio", "code": 400},
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err == nil || !strings.Contains(err.Error(), "bad audio") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestElevenLabsTranscription_DiarizeDefaultsWithProviderOptions(t *testing.T) {
	cases := []struct {
		name string
		opts map[string]interface{}
		want string
	}{
		{"no options", nil, "true"},
		{"empty options", map[string]interface{}{}, "true"},
		{"unrelated option", map[string]interface{}{"languageCode": "en"}, "true"},
		{"explicitly disabled", map[string]interface{}{"diarize": false}, "false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := map[string]string{}
			srv := transcriptionTestServer(t, &capture)
			defer srv.Close()
			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			m, _ := p.TranscriptionModel(ModelScribeV1)
			o := &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"}
			if tc.opts != nil {
				o.ProviderOptions = map[string]interface{}{"elevenlabs": tc.opts}
			}
			if _, err := m.DoTranscribe(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			if capture["diarize"] != tc.want {
				t.Fatalf("diarize = %q, want %q", capture["diarize"], tc.want)
			}
		})
	}
}

func TestElevenLabsTranscription_PreservesWordMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"language_code":"eng","language_probability":1,"text":"hi (laughter)","words":[` +
			`{"text":"hi","type":"word","start":0,"end":0.4,"speaker_id":"speaker_1"},` +
			`{"text":" ","type":"spacing","start":0.4,"end":0.5},` +
			`{"text":"(laughter)","type":"audio_event","start":0.5,"end":1.2,"speaker_id":"speaker_1"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"elevenlabs": map[string]interface{}{"words": []interface{}{
		map[string]interface{}{"text": "hi", "type": "word", "start": 0.0, "end": 0.4, "speaker_id": "speaker_1"},
		map[string]interface{}{"text": " ", "type": "spacing", "start": 0.4, "end": 0.5},
		map[string]interface{}{"text": "(laughter)", "type": "audio_event", "start": 0.5, "end": 1.2, "speaker_id": "speaker_1"},
	}}}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("providerMetadata = %#v", res.ProviderMetadata)
	}
}

func TestElevenLabsAudioExtensionMatchesMediaTypeToExtension(t *testing.T) {
	for in, want := range map[string]string{"audio/mpeg": "mp3", "audio/x-wav": "wav", "audio/opus": "ogg", "audio/mp4": "m4a", "audio/x-m4a": "m4a", "audio/flac": "flac", "audio/aac": "aac", "": ""} {
		if got := elevenLabsAudioExtension(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// TS: durationInSeconds is response.audio_duration_secs ??
// response.words?.at(-1)?.end ?? undefined (#23804).
func TestElevenLabsTranscription_Duration(t *testing.T) {
	words := []map[string]interface{}{{"text": "Hello", "type": "word", "start": 0.0, "end": 2.06}}
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name          string
		audioDuration interface{} // nil omits the key; "null" sends JSON null
		words         interface{}
		want          *float64
	}{
		{"includes trailing silence", 4.24, words, f(4.24)},
		{"does not require word timestamps", 4.24, nil, f(4.24)},
		{"preserves a reported zero", 0.0, words, f(0)},
		{"falls back when audio duration is absent", nil, words, f(2.06)},
		{"falls back when audio duration is null", "null", words, f(2.06)},
		{"no duration anywhere", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := map[string]interface{}{"language_code": "en", "language_probability": 1, "text": "Hello", "words": tc.words}
				switch v := tc.audioDuration.(type) {
				case nil:
				case string:
					body["audio_duration_secs"] = nil
				default:
					body["audio_duration_secs"] = v
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()
			m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel(ModelScribeV1)
			result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == nil && result.DurationInSeconds != nil:
				t.Fatalf("duration = %v, want nil", *result.DurationInSeconds)
			case tc.want != nil && (result.DurationInSeconds == nil || *result.DurationInSeconds != *tc.want):
				t.Fatalf("duration = %v, want %v", result.DurationInSeconds, *tc.want)
			}
		})
	}
}
