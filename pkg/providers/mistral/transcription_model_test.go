package mistral

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestMistralTranscriptionModelDoTranscribe guards 4e116c9: batch Voxtral
// transcription via multipart/form-data to /v1/audio/transcriptions.
func TestMistralTranscriptionModelDoTranscribe(t *testing.T) {
	var seenPath string
	var seenFields map[string][]string
	var seenFileBytes []byte

	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("bad content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		seenFields = map[string][]string{}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("multipart read error: %v", err)
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "file" {
				seenFileBytes = data
				continue
			}
			seenFields[part.FormName()] = append(seenFields[part.FormName()], string(data))
		}
		return mistralJSONResponse(`{
			"model": "voxtral-mini-latest",
			"text": "hello world",
			"language": "en",
			"segments": [{"text":"hello world","start":0,"end":1.5}],
			"usage": {"prompt_tokens": 5, "prompt_audio_seconds": 1.5}
		}`), nil
	})

	m := NewTranscriptionModel(p, "voxtral-mini-latest")
	if m.SpecificationVersion() != "v4" || m.Provider() != "mistral" || m.ModelID() != "voxtral-mini-latest" {
		t.Fatalf("metadata mismatch")
	}

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("raw-audio-bytes"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"mistral": map[string]interface{}{
				"temperature": 0.2,
				"diarize":     true,
				"contextBias": []interface{}{"acme_corp"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if seenPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", seenPath)
	}
	if string(seenFileBytes) != "raw-audio-bytes" {
		t.Fatalf("file bytes = %q, want raw-audio-bytes", seenFileBytes)
	}
	if got := seenFields["model"]; len(got) != 1 || got[0] != "voxtral-mini-latest" {
		t.Fatalf("model field = %#v", got)
	}
	if got := seenFields["temperature"]; len(got) != 1 || got[0] != "0.2" {
		t.Fatalf("temperature field = %#v", got)
	}
	if got := seenFields["diarize"]; len(got) != 1 || got[0] != "true" {
		t.Fatalf("diarize field = %#v", got)
	}
	if got := seenFields["context_bias"]; len(got) != 1 || got[0] != "acme_corp" {
		t.Fatalf("context_bias field = %#v", got)
	}

	if result.Text != "hello world" || result.Language != "en" {
		t.Fatalf("result mismatch: %#v", result)
	}
	if len(result.Segments) != 1 || result.Segments[0].End != 1.5 {
		t.Fatalf("segments mismatch: %#v", result.Segments)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 1.5 {
		t.Fatalf("durationInSeconds mismatch: %#v", result.DurationInSeconds)
	}
	if result.Usage.DurationSeconds != 1.5 {
		t.Fatalf("usage duration mismatch: %#v", result.Usage)
	}
	metadata, ok := result.ProviderMetadata["mistral"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mistral provider metadata, got %#v", result.ProviderMetadata)
	}
	usage, ok := metadata["usage"].(map[string]interface{})
	if !ok || usage["promptTokens"] != 5 {
		t.Fatalf("provider metadata usage = %#v", metadata["usage"])
	}
	// Ported from TS mistral-transcription-model.test.ts
	// "expect(result.usage).toStrictEqual(transcriptionResponse.usage)" (TS
	// 8c659885c5 / #21427): the raw wire-format usage object (snake_case
	// keys), distinct from the camelCased providerMetadata.mistral.usage.
	if got, want := result.ProviderUsage["prompt_tokens"], 5; got != want {
		t.Fatalf("ProviderUsage[prompt_tokens] = %v, want %v", got, want)
	}
	if got, want := result.ProviderUsage["prompt_audio_seconds"], 1.5; got != want {
		t.Fatalf("ProviderUsage[prompt_audio_seconds] = %v, want %v", got, want)
	}
}

// TestMistralTranscriptionLanguageAndTimestampGranularitiesMutuallyExclusive
// guards the InvalidArgumentError TS raises for this combination.
func TestMistralTranscriptionLanguageAndTimestampGranularitiesMutuallyExclusive(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("should not perform an HTTP request when validation fails")
		return nil, nil
	})
	m := NewTranscriptionModel(p, "voxtral-mini-latest")

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("x"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"mistral": map[string]interface{}{
				"language":               "en",
				"timestampGranularities": []interface{}{"word"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// TestMistralTranscriptionAudioBase64 verifies AudioBase64 input is decoded
// and sent as the file part.
func TestMistralTranscriptionAudioBase64(t *testing.T) {
	var seenFileBytes []byte
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "file" {
				seenFileBytes, _ = io.ReadAll(part)
			}
		}
		return mistralJSONResponse(`{"model":"voxtral-mini-latest","text":"hi"}`), nil
	})
	m := NewTranscriptionModel(p, "voxtral-mini-latest")

	// "aGVsbG8=" is base64 for "hello".
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: "aGVsbG8=",
		MimeType:    "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if string(seenFileBytes) != "hello" {
		t.Fatalf("file bytes = %q, want hello", seenFileBytes)
	}
}
