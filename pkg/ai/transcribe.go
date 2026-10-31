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
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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
	// DownloadWithMetadata downloads AudioURL and also returns the response
	// media type, which then takes precedence over magic-byte detection (TS
	// transcribe: downloadedMediaType ?? detectMediaType(...) ?? 'audio/wav').
	// Download wins when both are set. When neither is set, the default
	// download (2 GiB limit) is used and its Content-Type is honored.
	DownloadWithMetadata URLDownloadWithMetadataFunction
	MaxRetries           *int

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// ExperimentalTelemetry configures observability for this operation.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings
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
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	callID := newCallID()
	// TS fires the start event (without audio size/media type) and then the
	// error event when the audio cannot be prepared (download or decode
	// failure), before rethrowing.
	failPreparation := func(err error) (*TranscribeResult, error) {
		prepCtx := telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
			CallID:        callID,
			OperationType: "ai.transcribe",
			ModelProvider: opts.Model.Provider(),
			ModelID:       opts.Model.ModelID(),
			Settings:      opts.ExperimentalTelemetry,
			Headers:       opts.Headers,
		})
		telemetry.FireOnError(prepCtx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, CallID: callID, Error: err})
		return nil, err
	}
	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := decodeTranscriptionAudioBase64(opts.AudioBase64)
		if err != nil {
			return failPreparation(err)
		}
		audio = decoded
	}
	var downloadedMediaType string
	if len(audio) == 0 && opts.AudioURL != "" {
		if opts.Download != nil {
			data, err := opts.Download(ctx, opts.AudioURL)
			if err != nil {
				return failPreparation(err)
			}
			audio = data
		} else {
			download := opts.DownloadWithMetadata
			if download == nil {
				download = CreateURLDownloadWithMetadata(nil)
			}
			result, err := download(ctx, opts.AudioURL)
			if err != nil {
				return failPreparation(err)
			}
			if result != nil {
				audio = result.Data
				downloadedMediaType = result.MediaType
			}
		}
	}
	// TS does not reject empty audio; only a missing source is an error.
	if audio == nil && opts.Audio == nil && opts.AudioBase64 == "" && opts.AudioURL == "" {
		return nil, fmt.Errorf("audio is required")
	}
	mediaType := opts.MimeType
	if mediaType == "" {
		mediaType = downloadedMediaType
	}
	if mediaType == "" {
		mediaType = detectTranscriptionMediaType(audio)
	}
	audioByteLength := int64(len(audio))
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		CallID:          callID,
		OperationType:   "ai.transcribe",
		ModelProvider:   opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		Settings:        opts.ExperimentalTelemetry,
		AudioByteLength: &audioByteLength,
		AudioMediaType:  mediaType,
		Headers:         opts.Headers,
	})
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
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, CallID: callID, Error: err})
		return nil, err
	}
	if raw == nil {
		var responses []TranscriptionModelResponseMetadata
		if raw != nil {
			responses = []TranscriptionModelResponseMetadata{transcriptionResponseMetadata(raw.Response, opts.Model)}
		}
		noTranscriptErr := &NoTranscriptGeneratedError{Responses: responses}
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, CallID: callID, Error: noTranscriptErr})
		return nil, noTranscriptErr
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
	logModelWarnings(warnings, opts.Model.Provider(), opts.Model.ModelID())
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		CallID:           callID,
		OperationType:    "ai.transcribe",
		ModelProvider:    opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Settings:         opts.ExperimentalTelemetry,
		Text:             raw.Text,
		AudioByteLength:  &audioByteLength,
		AudioMediaType:   mediaType,
		ProviderMetadata: providerMetadata,
		ProviderUsage:    raw.ProviderUsage,
	})
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
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{
		Type:    "deprecated",
		Setting: "experimental_transcribe",
		Message: "Use transcribe instead.",
	}}})
	return Transcribe(ctx, opts)
}

// detectTranscriptionMediaType mirrors the TypeScript SDK's
// `detectMediaType({data, topLevelType: 'audio'}) ?? 'audio/wav'`. Using the
// "audio" top-level table (rather than the generic, no-topLevelType scan)
// means MP4-container audio such as M4A is correctly reported as audio/mp4
// instead of being dropped because a generic scan can't tell an MP4 audio
// container apart from video/mp4 (TS detectMediaType, 76cb673).
func detectTranscriptionMediaType(data []byte) string {
	if mediaType, ok := fileutil.DetectMediaTypeSignature(data, "audio"); ok {
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
