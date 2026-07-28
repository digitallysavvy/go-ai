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
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// TranscribeOptions contains options for speech-to-text transcription.
type TranscribeOptions struct {
	Model provider.TranscriptionModel

	Audio []byte
	// AudioBase64 accepts TypeScript DataContent string input. When set, it is
	// decoded to bytes before media type detection and provider invocation.
	AudioBase64 string
	AudioURL    string

	MimeType        string
	ProviderOptions map[string]interface{}
	Headers         map[string]string
	Download        URLDownloadFunction
	MaxRetries      *int
}

// TranscribeResult contains speech transcription output.
type TranscribeResult struct {
	Text              string                               `json:"text"`
	Segments          []TranscriptionSegment               `json:"segments"`
	Language          string                               `json:"language,omitempty"`
	DurationInSeconds *float64                             `json:"durationInSeconds,omitempty"`
	Warnings          []types.Warning                      `json:"warnings"`
	Responses         []TranscriptionModelResponseMetadata `json:"responses"`
	ProviderMetadata  map[string]interface{}               `json:"providerMetadata"`
}

type TranscriptionSegment struct {
	Text        string  `json:"text"`
	StartSecond float64 `json:"startSecond"`
	EndSecond   float64 `json:"endSecond"`
}

// TranscriptionModelResponseMetadata contains metadata for a transcription model call.
type TranscriptionModelResponseMetadata struct {
	Timestamp time.Time         `json:"timestamp"`
	ModelID   string            `json:"modelId"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      interface{}       `json:"body,omitempty"`
}

// Experimental_TranscriptionResult mirrors the TypeScript SDK's deprecated
// Experimental_TranscriptionResult alias.
//
// Deprecated: use TranscribeResult.
type Experimental_TranscriptionResult = TranscribeResult

// NoTranscriptGeneratedError is returned when a transcription model returns no text.
type NoTranscriptGeneratedError struct {
	Responses []TranscriptionModelResponseMetadata
}

func (e *NoTranscriptGeneratedError) Error() string {
	return "No transcript generated."
}

// IsNoTranscriptGeneratedError reports whether err is a NoTranscriptGeneratedError.
func IsNoTranscriptGeneratedError(err error) bool {
	var target *NoTranscriptGeneratedError
	return errors.As(err, &target)
}

// Transcribe converts audio to text.
func Transcribe(ctx context.Context, opts TranscribeOptions) (*TranscribeResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := decodeTranscriptionAudioBase64(opts.AudioBase64)
		if err != nil {
			return nil, err
		}
		audio = decoded
	}
	if len(audio) == 0 && opts.AudioURL != "" {
		download := opts.Download
		if download == nil {
			download = CreateURLDownload(nil)
		}
		data, err := download(ctx, opts.AudioURL)
		if err != nil {
			return nil, err
		}
		audio = data
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("audio is required")
	}
	mediaType := opts.MimeType
	if mediaType == "" {
		mediaType = detectTranscriptionMediaType(audio)
	}
	providerOptions := opts.ProviderOptions
	if providerOptions == nil {
		providerOptions = map[string]interface{}{}
	}
	callOptions := &provider.TranscriptionOptions{
		Audio:           audio,
		MimeType:        mediaType,
		ProviderOptions: providerOptions,
		Headers:         transcribeHeadersWithUserAgent(opts.Headers),
	}
	maxRetries := preparedMaxRetries(opts.MaxRetries)
	var raw *types.TranscriptionResult
	var err error
	if maxRetries == 0 {
		raw, err = opts.Model.DoTranscribe(ctx, callOptions)
	} else {
		err = retryutil.Do(ctx, retryutil.Config{
			MaxRetries:   maxRetries,
			InitialDelay: 2 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   2,
			Jitter:       false,
			ShouldRetry:  isGatewayCallRetryable,
		}, func(retryCtx context.Context) error {
			var transcribeErr error
			raw, transcribeErr = opts.Model.DoTranscribe(retryCtx, callOptions)
			return transcribeErr
		})
	}
	if err != nil {
		return nil, err
	}
	if raw == nil || raw.Text == "" {
		var responses []TranscriptionModelResponseMetadata
		if raw != nil {
			responses = []TranscriptionModelResponseMetadata{transcriptionResponseMetadata(raw.Response, opts.Model)}
		}
		return nil, &NoTranscriptGeneratedError{Responses: responses}
	}
	segments := make([]TranscriptionSegment, 0, len(raw.Segments))
	for _, ts := range raw.Segments {
		segments = append(segments, TranscriptionSegment{
			Text:        ts.Text,
			StartSecond: ts.Start,
			EndSecond:   ts.End,
		})
	}
	responses := []TranscriptionModelResponseMetadata{transcriptionResponseMetadata(raw.Response, opts.Model)}
	warnings := raw.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	providerMetadata := raw.ProviderMetadata
	if providerMetadata == nil {
		providerMetadata = map[string]interface{}{}
	}
	return &TranscribeResult{
		Text:              raw.Text,
		Segments:          segments,
		Language:          raw.Language,
		DurationInSeconds: raw.DurationInSeconds,
		Warnings:          warnings,
		Responses:         responses,
		ProviderMetadata:  providerMetadata,
	}, nil
}

func transcriptionResponseMetadata(response *types.ResponseMetadata, model provider.TranscriptionModel) TranscriptionModelResponseMetadata {
	if response == nil {
		metadata := TranscriptionModelResponseMetadata{Timestamp: time.Now()}
		if model != nil {
			metadata.ModelID = model.ModelID()
		}
		return metadata
	}
	return TranscriptionModelResponseMetadata{
		Timestamp: response.Timestamp,
		ModelID:   response.ModelID,
		Headers:   response.Headers,
		Body:      response.Body,
	}
}

// ExperimentalTranscribe mirrors the TypeScript SDK's deprecated
// experimental_transcribe export.
//
// Deprecated: use Transcribe.
func ExperimentalTranscribe(ctx context.Context, opts TranscribeOptions) (*TranscribeResult, error) {
	return Transcribe(ctx, opts)
}

func detectTranscriptionMediaType(data []byte) string {
	mediaType := fileutil.DetectMediaType(data).MimeType
	if mediaType == "audio/wave" {
		return "audio/wav"
	}
	if strings.HasPrefix(mediaType, "audio/") {
		return mediaType
	}
	return "audio/wav"
}

func transcribeHeadersWithUserAgent(headers map[string]string) map[string]string {
	return version.WithUserAgentSuffix(headers, version.UserAgent())
}

func decodeTranscriptionAudioBase64(content string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(content)
	if decoded, err := base64.StdEncoding.DecodeString(normalized); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(normalized); err == nil {
		return decoded, nil
	}
	return nil, &providererrors.InvalidArgumentError{
		Field:   "audio",
		Message: "invalid base64 audio data",
	}
}
