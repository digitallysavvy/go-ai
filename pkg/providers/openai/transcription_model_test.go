package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGetExtensionFromMimeType(t *testing.T) {
	tests := map[string]string{
		"audio/mpeg": "mp3",
		"audio/mp3":  "mp3",
		"audio/wav":  "wav",
		"audio/webm": "webm",
		"audio/mp4":  "m4a",
		"audio/m4a":  "m4a",
		"audio/ogg":  "audio",
	}
	for in, want := range tests {
		if got := getExtensionFromMimeType(in); got != want {
			t.Fatalf("mime %q -> %q, want %q", in, got, want)
		}
	}
}

func TestTranscriptionBuildMultipartBody(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "whisper-1")

	body, ct, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:      []byte("audio"),
		MimeType:   "audio/mpeg",
		Language:   "fr",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	if !strings.Contains(ct, "multipart/form-data") {
		t.Fatalf("content type = %q", ct)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="model"`) || !strings.Contains(blob, "whisper-1") {
		t.Fatalf("missing model field: %s", blob)
	}
	if !strings.Contains(blob, `name="language"`) || !strings.Contains(blob, "fr") {
		t.Fatalf("missing language field: %s", blob)
	}
	if !strings.Contains(blob, `name="timestamp_granularities[]"`) || !strings.Contains(blob, `name="response_format"`) || !strings.Contains(blob, "verbose_json") {
		t.Fatalf("missing timestamp or response format fields: %s", blob)
	}
}

// TestTranscriptionDiarizeModelDefaults ports abb9ebf: gpt-4o-transcribe-diarize
// defaults response_format to diarized_json and chunking_strategy to "auto".
func TestTranscriptionDiarizeModelDefaults(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "gpt-4o-transcribe-diarize")

	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="response_format"`) || !strings.Contains(blob, "diarized_json") {
		t.Fatalf("expected diarized_json response_format: %s", blob)
	}
	if !strings.Contains(blob, `name="chunking_strategy"`) || !strings.Contains(blob, "auto") {
		t.Fatalf("expected auto chunking_strategy: %s", blob)
	}
}

// TestTranscriptionGpt4oTranscribeDefaultsToJSON covers the
// isGpt4oTranscribeModel branch (json, not verbose_json).
func TestTranscriptionGpt4oTranscribeDefaultsToJSON(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "gpt-4o-transcribe")

	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="response_format"`) {
		t.Fatalf("missing response_format: %s", blob)
	}
	if strings.Contains(blob, "verbose_json") || strings.Contains(blob, "diarized_json") {
		t.Fatalf("expected plain json response_format: %s", blob)
	}
}

// TestTranscriptionResponseFormatProviderOptionOverride covers the
// providerOptions.openai.responseFormat override.
func TestTranscriptionResponseFormatProviderOptionOverride(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "gpt-4o-transcribe")

	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"responseFormat": "verbose_json"},
		},
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, "verbose_json") {
		t.Fatalf("expected provider-option response_format override: %s", blob)
	}
}

// TestTranscriptionChunkingStrategyServerVAD covers the object form of
// chunkingStrategy (server_vad with threshold/prefixPaddingMs/silenceDurationMs).
func TestTranscriptionChunkingStrategyServerVAD(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "gpt-4o-transcribe-diarize")

	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"chunkingStrategy": map[string]interface{}{
					"type":              "server_vad",
					"threshold":         0.5,
					"prefixPaddingMs":   300,
					"silenceDurationMs": 200,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `"type":"server_vad"`) || !strings.Contains(blob, `"threshold":0.5`) ||
		!strings.Contains(blob, `"prefix_padding_ms":300`) || !strings.Contains(blob, `"silence_duration_ms":200`) {
		t.Fatalf("chunking_strategy body mismatch: %s", blob)
	}
}

// TestTranscriptionDiarizedSegmentsProviderMetadata covers the diarized
// segments -> providerMetadata.openai.segments mapping.
func TestTranscriptionDiarizedSegmentsProviderMetadata(t *testing.T) {
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"hello there",
			"duration":2.0,
			"segments":[
				{"text":"hello","start":0.0,"end":1.0,"speaker":"A"},
				{"text":"there","start":1.0,"end":2.0,"speaker":"B"}
			]
		}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL + "/v1"})
	m := NewTranscriptionModel(p, "gpt-4o-transcribe-diarize")

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("segments = %#v", result.Segments)
	}
	openaiMeta, ok := result.ProviderMetadata["openai"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.openai missing: %#v", result.ProviderMetadata)
	}
	segments, ok := openaiMeta["segments"].([]map[string]interface{})
	if !ok || len(segments) != 2 {
		t.Fatalf("diarized segments = %#v", openaiMeta["segments"])
	}
	if segments[0]["speaker"] != "A" || segments[1]["speaker"] != "B" {
		t.Fatalf("speaker mismatch: %#v", segments)
	}
}

func TestTranscriptionDoTranscribeSuccessAndErrors(t *testing.T) {
	var seenPath string
	var seenContentType string
	var seenBody string
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello","duration":1.2}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL + "/v1"})
	m := NewTranscriptionModel(p, "whisper-1")

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if seenPath != "/v1/audio/transcriptions" || !strings.Contains(seenContentType, "multipart/form-data") {
		t.Fatalf("request mismatch path=%q contentType=%q", seenPath, seenContentType)
	}
	if !strings.Contains(seenBody, `name="response_format"`) || !strings.Contains(seenBody, "json") {
		t.Fatalf("response format field missing: %s", seenBody)
	}
	if result.Text != "hello" || result.Usage.DurationSeconds != 1.2 {
		t.Fatalf("result mismatch: %#v", result)
	}

	errorServerURL, closeErr := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad"))
	}))
	defer closeErr()
	mErr := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: errorServerURL + "/v1"}), "whisper-1")
	if _, err := mErr.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err == nil || !strings.Contains(err.Error(), "returned status 400") {
		t.Fatalf("expected status error, got %v", err)
	}

	badJSONServerURL, closeBadJSON := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer closeBadJSON()
	mBadJSON := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: badJSONServerURL + "/v1"}), "whisper-1")
	if _, err := mBadJSON.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err == nil || !strings.Contains(err.Error(), "failed to decode transcription response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

// TestTranscriptionExtractsUsage is ported from TS
// openai-transcription-model.test.ts "should extract usage" (TS 8c659885c5 /
// #21427): the response's usage object (whatever shape OpenAI reports) is
// surfaced verbatim on the result.
func TestTranscriptionExtractsUsage(t *testing.T) {
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello","duration":37,"usage":{"type":"duration","seconds":37}}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL + "/v1"})
	m := NewTranscriptionModel(p, "whisper-1")

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	want := map[string]interface{}{"type": "duration", "seconds": float64(37)}
	if got := result.ProviderUsage; got["type"] != want["type"] || got["seconds"] != want["seconds"] {
		t.Fatalf("ProviderUsage = %#v, want %#v", got, want)
	}
}

// TestTranscriptionWhisper1AlwaysUsesVerboseJSON ports TS getArgs: whisper-1
// unconditionally gets response_format=verbose_json regardless of whether
// timestamps were requested, and a providerOptions.openai.responseFormat
// override does not apply to it (the TS conditional spread that lets other
// models opt into a different responseFormat is gated on
// `this.modelId !== 'whisper-1'`).
func TestTranscriptionWhisper1AlwaysUsesVerboseJSON(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "whisper-1")

	// No timestamps requested: still verbose_json, not "json".
	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="response_format"`) || !strings.Contains(blob, "verbose_json") {
		t.Fatalf("expected verbose_json response_format even without timestamps: %s", blob)
	}

	// An explicit responseFormat provider option override is ignored for whisper-1.
	body, _, err = m.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"responseFormat": "json"},
		},
	})
	if err != nil {
		t.Fatalf("buildMultipartBody error = %v", err)
	}
	raw, _ = io.ReadAll(body)
	blob = string(raw)
	if !strings.Contains(blob, "verbose_json") {
		t.Fatalf("expected verbose_json response_format for whisper-1 even with responseFormat override: %s", blob)
	}
}
