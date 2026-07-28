package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// GeneratedAudioFile mirrors the TypeScript SDK's GeneratedAudioFile export.
type GeneratedAudioFile struct {
	// Data is the raw generated audio bytes.
	Data []byte `json:"data,omitempty"`

	// URL is the URL to the audio file, when a provider returns one.
	URL string `json:"url,omitempty"`

	// MediaType is the IANA media type of the generated audio.
	MediaType string `json:"mediaType"`

	// Format is the audio format, derived from MediaType. audio/mpeg maps to mp3.
	Format string `json:"format"`
}

// Base64 returns the generated audio bytes as a base64 string, matching the
// TypeScript GeneratedAudioFile base64 accessor.
func (f GeneratedAudioFile) Base64() string {
	return base64.StdEncoding.EncodeToString(f.Data)
}

// Uint8Array returns the generated audio bytes, matching the TypeScript
// GeneratedAudioFile uint8Array accessor.
func (f GeneratedAudioFile) Uint8Array() []byte {
	return f.Data
}

// GenerateSpeechOptions contains options for speech generation.
type GenerateSpeechOptions struct {
	Model provider.SpeechModel
	Text  string
	Voice string
	Speed *float64

	OutputFormat    string
	Instructions    string
	Language        string
	ProviderOptions map[string]interface{}
	Headers         map[string]string
	MaxRetries      *int
}

// GenerateSpeechResult contains generated speech audio.
type GenerateSpeechResult struct {
	Audio            GeneratedAudioFile            `json:"audio"`
	Warnings         []types.Warning               `json:"warnings"`
	Responses        []SpeechModelResponseMetadata `json:"responses"`
	ProviderMetadata map[string]interface{}        `json:"providerMetadata"`
}

// SpeechModelResponseMetadata contains metadata for a speech model call.
type SpeechModelResponseMetadata struct {
	Timestamp time.Time         `json:"timestamp"`
	ModelID   string            `json:"modelId"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      interface{}       `json:"body,omitempty"`
}

// Experimental_SpeechResult mirrors the TypeScript SDK's deprecated
// Experimental_SpeechResult alias.
//
// Deprecated: use GenerateSpeechResult.
type Experimental_SpeechResult = GenerateSpeechResult

// NoSpeechGeneratedError is returned when a speech model returns no audio.
type NoSpeechGeneratedError struct {
	Responses []SpeechModelResponseMetadata
}

func (e *NoSpeechGeneratedError) Error() string {
	return "No speech audio generated."
}

// IsNoSpeechGeneratedError reports whether err is a NoSpeechGeneratedError.
func IsNoSpeechGeneratedError(err error) bool {
	var target *NoSpeechGeneratedError
	return errors.As(err, &target)
}

// GenerateSpeech converts text to speech audio.
func GenerateSpeech(ctx context.Context, opts GenerateSpeechOptions) (*GenerateSpeechResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	providerOptions := opts.ProviderOptions
	if providerOptions == nil {
		providerOptions = map[string]interface{}{}
	}
	callOptions := &provider.SpeechGenerateOptions{
		Text:            opts.Text,
		Voice:           opts.Voice,
		Speed:           opts.Speed,
		OutputFormat:    opts.OutputFormat,
		Instructions:    opts.Instructions,
		Language:        opts.Language,
		ProviderOptions: providerOptions,
		Headers:         speechHeadersWithUserAgent(opts.Headers),
	}
	maxRetries := preparedMaxRetries(opts.MaxRetries)
	var raw *types.SpeechResult
	var err error
	if maxRetries == 0 {
		raw, err = opts.Model.DoGenerate(ctx, callOptions)
	} else {
		err = retryutil.Do(ctx, retryutil.Config{
			MaxRetries:   maxRetries,
			InitialDelay: 2 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   2,
			Jitter:       false,
			ShouldRetry:  isGatewayCallRetryable,
		}, func(retryCtx context.Context) error {
			var genErr error
			raw, genErr = opts.Model.DoGenerate(retryCtx, callOptions)
			return genErr
		})
	}
	if err != nil {
		return nil, err
	}
	var responses []SpeechModelResponseMetadata
	if raw != nil {
		responses = []SpeechModelResponseMetadata{speechResponseMetadata(raw.Response, opts.Model)}
	}
	if raw == nil || len(raw.Audio) == 0 {
		return nil, &NoSpeechGeneratedError{Responses: responses}
	}
	providerMetadata := raw.ProviderMetadata
	if providerMetadata == nil {
		providerMetadata = map[string]interface{}{}
	}
	warnings := raw.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	mediaType := resolveGeneratedSpeechMediaType(raw.Audio)
	return &GenerateSpeechResult{
		Audio: GeneratedAudioFile{
			Data:      raw.Audio,
			MediaType: mediaType,
			Format:    generatedAudioFormat(mediaType),
		},
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: providerMetadata,
	}, nil
}

func speechResponseMetadata(response *types.ResponseMetadata, model provider.SpeechModel) SpeechModelResponseMetadata {
	if response == nil {
		metadata := SpeechModelResponseMetadata{Timestamp: time.Now()}
		if model != nil {
			metadata.ModelID = model.ModelID()
		}
		return metadata
	}
	return SpeechModelResponseMetadata{
		Timestamp: response.Timestamp,
		ModelID:   response.ModelID,
		Headers:   response.Headers,
		Body:      response.Body,
	}
}

// ExperimentalGenerateSpeech mirrors the TypeScript SDK's deprecated
// experimental_generateSpeech export.
//
// Deprecated: use GenerateSpeech.
func ExperimentalGenerateSpeech(ctx context.Context, opts GenerateSpeechOptions) (*GenerateSpeechResult, error) {
	return GenerateSpeech(ctx, opts)
}

func speechHeadersWithUserAgent(headers map[string]string) map[string]string {
	return version.WithUserAgentSuffix(headers, version.UserAgent())
}

func resolveGeneratedSpeechMediaType(data []byte) string {
	mediaType := fileutil.DetectMediaType(data).MimeType
	if strings.HasPrefix(mediaType, "audio/") {
		return mediaType
	}
	return "audio/mp3"
}

func generatedAudioFormat(mediaType string) string {
	if mediaType == "audio/mpeg" {
		return "mp3"
	}
	parts := strings.Split(mediaType, "/")
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}
	return "mp3"
}
