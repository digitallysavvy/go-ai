package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateSpeechForwardsProviderResponseMetadata(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	var capturedHeaders map[string]string
	var capturedProviderOptions map[string]interface{}
	model := &testutil.MockSpeechModel{
		ModelName: "speech-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			capturedHeaders = opts.Headers
			capturedProviderOptions = opts.ProviderOptions
			return &types.SpeechResult{
				Audio:   []byte("audio"),
				Request: &types.StepRequest{Body: `{"input":"hello"}`},
				Response: &types.ResponseMetadata{
					ID:        "provider-only-id",
					Timestamp: timestamp,
					ModelID:   "speech-model",
					Headers:   map[string]string{"X-Request": "speech-1"},
					Body:      []byte(`{"ok":true}`),
				},
			}, nil
		},
	}

	result, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model,
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech error = %v", err)
	}
	if capturedHeaders["user-agent"] != "go-ai/0.5.0" {
		t.Fatalf("user-agent = %q", capturedHeaders["user-agent"])
	}
	if capturedProviderOptions == nil || len(capturedProviderOptions) != 0 {
		t.Fatalf("provider options = %#v, want empty map", capturedProviderOptions)
	}
	if len(result.Responses) != 1 || result.Responses[0].Timestamp != timestamp || result.Responses[0].Headers["X-Request"] != "speech-1" {
		t.Fatalf("responses = %#v", result.Responses)
	}
	if result.ProviderMetadata == nil || len(result.ProviderMetadata) != 0 {
		t.Fatalf("provider metadata = %#v, want empty map", result.ProviderMetadata)
	}
	if result.Audio.MediaType != "audio/mp3" {
		t.Fatalf("media type = %q, want audio/mp3 fallback", result.Audio.MediaType)
	}
	if string(result.Responses[0].Body.([]byte)) != `{"ok":true}` {
		t.Fatalf("response body = %#v", result.Responses[0].Body)
	}
	encoded, err := json.Marshal(result.Responses[0])
	if err != nil {
		t.Fatalf("marshal response metadata: %v", err)
	}
	if strings.Contains(string(encoded), `"id"`) {
		t.Fatalf("speech response metadata JSON = %s, must not expose provider-only id", encoded)
	}
	if !strings.Contains(string(encoded), `"body"`) {
		t.Fatalf("speech response metadata JSON = %s, want body", encoded)
	}
}

func TestGenerateSpeechAllowsEmptyText(t *testing.T) {
	t.Parallel()

	called := false
	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			called = true
			if opts.Text != "" {
				t.Fatalf("text = %q, want empty string", opts.Text)
			}
			return &types.SpeechResult{
				Audio:    []byte("audio"),
				Response: &types.ResponseMetadata{ModelID: "speech-model"},
			}, nil
		},
	}

	if _, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{Model: model}); err != nil {
		t.Fatalf("GenerateSpeech error = %v", err)
	}
	if !called {
		t.Fatal("model was not called")
	}
}

func TestGenerateSpeechDefaultRetriesTransientProviderError(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			attempts++
			if attempts == 1 {
				return nil, &providererrors.ProviderError{
					Provider:        "mock",
					StatusCode:      500,
					Message:         "temporary",
					ResponseHeaders: map[string]string{"retry-after-ms": "0"},
				}
			}
			return &types.SpeechResult{
				Audio:    []byte("audio"),
				Response: &types.ResponseMetadata{ModelID: "speech-model"},
			}, nil
		},
	}

	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model,
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGenerateSpeechMaxRetriesZeroDisablesRetries(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			attempts++
			return nil, &providererrors.ProviderError{
				Provider:        "mock",
				StatusCode:      500,
				Message:         "temporary",
				ResponseHeaders: map[string]string{"retry-after-ms": "0"},
			}
		},
	}
	maxRetries := 0
	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model:      model,
		Text:       "hello",
		MaxRetries: &maxRetries,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestGenerateSpeechRejectsNegativeMaxRetriesWithInvalidArgumentError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockSpeechModel{}
	maxRetries := -1
	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model:      model,
		Text:       "hello",
		MaxRetries: &maxRetries,
	})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidArgumentError, got %T %v", err, err)
	}
	if invalid.Field != "maxRetries" || invalid.Message != "maxRetries must be >= 0" {
		t.Fatalf("invalid argument = %#v", invalid)
	}
}

func TestGenerateSpeechAppendsGoAIUserAgent(t *testing.T) {
	t.Parallel()

	var capturedHeaders map[string]string
	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			capturedHeaders = opts.Headers
			return &types.SpeechResult{
				Audio: []byte("audio"),
				Response: &types.ResponseMetadata{
					ModelID: "speech-model",
				},
			}, nil
		},
	}

	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model:   model,
		Text:    "hello",
		Headers: map[string]string{"user-agent": "custom-agent"},
	})
	if err != nil {
		t.Fatalf("GenerateSpeech error = %v", err)
	}
	if capturedHeaders["user-agent"] != "custom-agent go-ai/0.5.0" {
		t.Fatalf("user-agent = %q", capturedHeaders["user-agent"])
	}
	if _, ok := capturedHeaders["User-Agent"]; ok {
		t.Fatalf("unexpected generated User-Agent header: %#v", capturedHeaders)
	}
}

func TestGenerateSpeechNoAudioReturnsNoSpeechGeneratedErrorWithResponses(t *testing.T) {
	t.Parallel()

	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			return &types.SpeechResult{
				Response: &types.ResponseMetadata{
					ModelID: "speech-model",
					Headers: map[string]string{
						"X-Request": "speech-1",
					},
				},
			}, nil
		},
	}

	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model,
		Text:  "hello",
	})
	if !IsNoSpeechGeneratedError(err) {
		t.Fatalf("expected NoSpeechGeneratedError, got %T %v", err, err)
	}
	var noSpeech *NoSpeechGeneratedError
	if !errors.As(err, &noSpeech) {
		t.Fatalf("expected NoSpeechGeneratedError, got %T", err)
	}
	if len(noSpeech.Responses) != 1 || noSpeech.Responses[0].Headers["X-Request"] != "speech-1" {
		t.Fatalf("responses = %#v", noSpeech.Responses)
	}
}

func TestGeneratedAudioFormatMatchesTypeScript(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"audio/mpeg": "mp3",
		"audio/mp3":  "mp3",
		"audio/wav":  "wav",
		"audio/pcm":  "pcm",
		"":           "mp3",
	}
	for mediaType, want := range tests {
		if got := generatedAudioFormat(mediaType); got != want {
			t.Fatalf("generatedAudioFormat(%q) = %q, want %q", mediaType, got, want)
		}
	}
}

// TestResolveGeneratedSpeechMediaType ports TS generate-speech.ts's media
// type resolution priority (audit row e61cbd8 / WG11): sniffed bytes, then
// the response Content-Type header (audio/* only, params stripped), then
// outputFormat for headerless raw formats, then audio/mp3.
func TestResolveGeneratedSpeechMediaType(t *testing.T) {
	t.Parallel()

	wavBytes := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")

	tests := []struct {
		name         string
		data         []byte
		headers      map[string]string
		outputFormat string
		want         string
	}{
		{
			name: "sniffed from bytes takes precedence",
			data: wavBytes,
			headers: map[string]string{
				"Content-Type": "audio/mpeg",
			},
			outputFormat: "pcm",
			want:         "audio/wav", // TS detectMediaType topLevelType audio (detect-media-type.ts:134)
		},
		{
			name: "falls back to response Content-Type header",
			data: []byte("raw pcm bytes that can't be sniffed"),
			headers: map[string]string{
				"Content-Type": "audio/wav; codecs=1",
			},
			want: "audio/wav",
		},
		{
			name: "ignores a non-audio Content-Type header",
			data: []byte("raw pcm bytes that can't be sniffed"),
			headers: map[string]string{
				"content-type": "application/octet-stream",
			},
			outputFormat: "pcm",
			want:         "audio/pcm",
		},
		{
			name:         "falls back to outputFormat pcm",
			data:         []byte("raw pcm bytes that can't be sniffed"),
			outputFormat: "pcm",
			want:         "audio/pcm",
		},
		{
			name:         "falls back to outputFormat audio/mulaw",
			data:         []byte("raw mulaw bytes that can't be sniffed"),
			outputFormat: "mulaw",
			want:         "audio/mulaw",
		},
		{
			name:         "falls back to outputFormat audio/alaw",
			data:         []byte("raw alaw bytes that can't be sniffed"),
			outputFormat: "audio/alaw",
			want:         "audio/alaw",
		},
		{
			name:         "falls back to outputFormat audio/l16",
			data:         []byte("raw l16 bytes that can't be sniffed"),
			outputFormat: "audio/l16",
			want:         "audio/l16",
		},
		{
			name: "final fallback is audio/mp3",
			data: []byte("unrecognizable bytes"),
			want: "audio/mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveGeneratedSpeechMediaType(tt.data, tt.headers, tt.outputFormat)
			if got != tt.want {
				t.Errorf("resolveGeneratedSpeechMediaType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGenerateSpeechUsesOutputFormatWhenBytesUndetectableAndNoContentType
// exercises the resolution chain end-to-end through GenerateSpeech.
func TestGenerateSpeechUsesOutputFormatWhenBytesUndetectableAndNoContentType(t *testing.T) {
	t.Parallel()

	model := &testutil.MockSpeechModel{
		ModelName: "speech-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			return &types.SpeechResult{Audio: []byte("raw pcm bytes that can't be sniffed")}, nil
		},
	}

	result, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model:        model,
		Text:         "hello",
		OutputFormat: "pcm",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech error = %v", err)
	}
	if result.Audio.MediaType != "audio/pcm" {
		t.Fatalf("media type = %q, want audio/pcm", result.Audio.MediaType)
	}
}
