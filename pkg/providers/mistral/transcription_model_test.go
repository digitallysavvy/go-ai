package mistral

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
	if m.SpecificationVersion() != "v4" || m.Provider() != "mistral.transcription" || m.ModelID() != "voxtral-mini-latest" {
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

// Go-typed option values ([]string, int) must reach the form like their JSON
// equivalents instead of being silently dropped; invalid values are rejected
// like the TS zod schema.
func TestMistralTranscriptionTypedOptionsAreForwardedAndValidated(t *testing.T) {
	fields := map[string][]string{}
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			b, _ := io.ReadAll(part)
			fields[part.FormName()] = append(fields[part.FormName()], string(b))
		}
		return mistralJSONResponse(`{"model":"voxtral-mini-latest","text":"hi"}`), nil
	})
	m := NewTranscriptionModel(p, "voxtral-mini-latest")

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("x"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"timestampGranularities": []string{"word", "segment"},
			"temperature":            1,
			"contextBias":            []string{"a_b", "c"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fields["timestamp_granularities"]; len(got) != 2 || got[0] != "word" || got[1] != "segment" {
		t.Fatalf("timestamp_granularities = %v", got)
	}
	if got := fields["temperature"]; len(got) != 1 || got[0] != "1" {
		t.Fatalf("temperature = %v", got)
	}
	if got := fields["context_bias"]; len(got) != 2 {
		t.Fatalf("context_bias = %v", got)
	}

	for name, bad := range map[string]map[string]interface{}{
		"enum":        {"timestampGranularities": []string{"line"}},
		"empty entry": {"contextBias": []string{""}},
		"empty list":  {"timestampGranularities": []string{}},
		"temperature": {"temperature": "hot"},
	} {
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("x"), MimeType: "audio/wav",
			ProviderOptions: map[string]interface{}{"mistral": bad},
		})
		if err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

// TS builds mistralMetadata.usage whenever response.usage != null, even when
// it carries no known field.
func TestTranscriptionEmptyUsageStillProducesUsageMetadata(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return mistralJSONResponse(`{"model":"m","text":"hi","usage":{}}`), nil
	})
	result, err := NewTranscriptionModel(p, "m").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := result.ProviderMetadata["mistral"].(map[string]interface{})
	if usage, ok := meta["usage"].(map[string]interface{}); !ok || len(usage) != 0 {
		t.Fatalf("metadata = %#v", result.ProviderMetadata)
	}
}

// transcribeForm runs one transcription and returns the multipart fields.
func transcribeForm(t *testing.T, modelID string, mistralOpts map[string]interface{}, respBody string) (map[string][]string, interface{}, error) {
	t.Helper()
	fields := map[string][]string{}
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			b, _ := io.ReadAll(part)
			fields[part.FormName()] = append(fields[part.FormName()], string(b))
		}
		return mistralJSONResponse(respBody), nil
	})
	res, err := NewTranscriptionModel(p, modelID).DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("x"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"mistral": mistralOpts},
	})
	var lang interface{}
	if res != nil {
		lang = res.Language
	}
	return fields, lang, err
}

// Ported from TS #23781 (Voxtral 3 transcription).
func TestMistralTranscriptionVoxtral3(t *testing.T) {
	const okBody = `{"model":"m","text":"hi"}`

	t.Run("context bias accepts phrases, punctuation, and more than 100 entries", func(t *testing.T) {
		bias := []string{"AI SDK", "A conversation about Vercel, TypeScript, and speech."}
		for i := 0; i < 100; i++ {
			bias = append(bias, fmt.Sprintf("Product %d", i))
		}
		fields, _, err := transcribeForm(t, "voxtral-mini-transcribe-3-0", map[string]interface{}{"contextBias": bias}, okBody)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fields["context_bias"], bias) {
			t.Fatalf("context_bias = %v", fields["context_bias"])
		}
	})

	t.Run("legacy models reject language with timestamps", func(t *testing.T) {
		for _, id := range []string{"voxtral-mini-2507", "voxtral-mini-2602", "voxtral-mini-latest"} {
			_, _, err := transcribeForm(t, id, map[string]interface{}{"language": "en", "timestampGranularities": []string{"segment"}}, okBody)
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) {
				t.Errorf("%s: err = %v, want InvalidArgumentError", id, err)
			}
		}
	})

	t.Run("unrecognized models allow language with timestamps", func(t *testing.T) {
		for _, id := range []string{"voxtral-mini-transcribe-4-0", "voxtral-small-transcribe-3-1", "custom-transcription-model"} {
			fields, _, err := transcribeForm(t, id, map[string]interface{}{"language": "en", "timestampGranularities": []string{"segment"}}, okBody)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if fields["language"][0] != "en" || fields["timestamp_granularities"][0] != "segment" {
				t.Errorf("%s: fields = %v", id, fields)
			}
		}
	})

	t.Run("voxtral 3 allows language with segment timestamps and diarization", func(t *testing.T) {
		for _, id := range []string{"voxtral-mini-transcribe-3-0", "voxtral-mini-transcribe-3", "voxtral-mini-transcribe-latest", "voxtral-small-transcribe-3-0", "voxtral-small-transcribe-3", "voxtral-small-transcribe-latest"} {
			fields, _, err := transcribeForm(t, id, map[string]interface{}{
				"language": "en", "returnLanguage": false,
				"timestampGranularities": []string{"segment"}, "diarize": true,
			}, okBody)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if fields["return_language"][0] != "false" || fields["diarize"][0] != "true" || fields["timestamp_granularities"][0] != "segment" {
				t.Errorf("%s: fields = %v", id, fields)
			}
		}
	})

	t.Run("requests and returns the detected language", func(t *testing.T) {
		fields, lang, err := transcribeForm(t, "voxtral-mini-transcribe-3-0", map[string]interface{}{"returnLanguage": true}, `{"model":"m","text":"hi","language":"en"}`)
		if err != nil {
			t.Fatal(err)
		}
		if fields["return_language"][0] != "true" || lang != "en" {
			t.Fatalf("fields = %v lang = %v", fields, lang)
		}
	})

	t.Run("rejects language prediction when the language is pinned", func(t *testing.T) {
		_, _, err := transcribeForm(t, "voxtral-mini-transcribe-3-0", map[string]interface{}{"language": "en", "returnLanguage": true}, okBody)
		if err == nil || !strings.Contains(err.Error(), "providerOptions.mistral.language cannot be combined with providerOptions.mistral.returnLanguage") {
			t.Fatalf("err = %v", err)
		}
	})
}
