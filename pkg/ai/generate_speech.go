package ai

import (
	"context"
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
	Audio            types.GeneratedFile       `json:"audio"`
	Warnings         []types.Warning           `json:"warnings"`
	Responses        []*types.ResponseMetadata `json:"responses"`
	ProviderMetadata map[string]interface{}    `json:"providerMetadata"`
}

// NoSpeechGeneratedError is returned when a speech model returns no audio.
type NoSpeechGeneratedError struct {
	Responses []*types.ResponseMetadata
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
	var responses []*types.ResponseMetadata
	if raw != nil {
		responses = []*types.ResponseMetadata{raw.Response}
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
	return &GenerateSpeechResult{
		Audio: types.GeneratedFile{
			Data:      raw.Audio,
			MediaType: resolveGeneratedSpeechMediaType(raw.Audio),
		},
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: providerMetadata,
	}, nil
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
