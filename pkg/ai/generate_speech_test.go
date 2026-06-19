package ai

import (
	"context"
	"errors"
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
