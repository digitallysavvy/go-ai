package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// TimestampGranularities is the shared word / segment timing request
	// (provider.TranscriptionTimestampGranularityWord /
	// TranscriptionTimestampGranularitySegment), forwarded only when non-nil.
	// Supporting adapters map it onto the provider's native control when the
	// matching provider option is omitted (an explicit provider option wins)
	// and warn when they cannot return a requested granularity. An empty,
	// non-nil slice requests no shared granularity; provider defaults may
	// vary. Mirrors TS transcribe `timestampGranularities`.
	TimestampGranularities []string

	Headers map[string]string

	// Download fetches AudioURL. Setting Download or DownloadWithMetadata
	// forces a local download even when the model supports native URL
	// transcription (provider.TranscriptionURLModel).
	Download URLDownloadFunction
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
	Text     string                 `json:"text"`
	Segments []TranscriptionSegment `json:"segments"`
	// Words are word-level timings the provider reported, requested or
	// returned by default; nil when unavailable (TS TranscriptionResult.words).
	Words             []TranscriptionWord                  `json:"words,omitempty"`
	Language          string                               `json:"language,omitempty"`
	DurationInSeconds *float64                             `json:"durationInSeconds,omitempty"`
	Warnings          []types.Warning                      `json:"warnings"`
	Responses         []TranscriptionModelResponseMetadata `json:"responses"`
	ProviderMetadata  map[string]interface{}               `json:"providerMetadata"`
}

// TranscriptionWord is one word-level timing in TranscribeResult.Words
// ({text, startSecond, endSecond}).
type TranscriptionWord = types.TranscriptionWord

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
	// TS createStartEvent: maxRetries (the prepared value), providerOptions
	// (default {}) and timestampGranularities (transcription-events.ts).
	telemetryMaxRetries := preparedMaxRetries(opts.MaxRetries)
	providerOptions := opts.ProviderOptions
	if providerOptions == nil {
		providerOptions = map[string]interface{}{}
	}
	// TS fires the start event (without audio size/media type) and then the
	// error event when the audio cannot be prepared (download or decode
	// failure), before rethrowing.
	failPreparation := func(err error) (*TranscribeResult, error) {
		prepCtx := telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
			CallID:                 callID,
			OperationType:          "ai.transcribe",
			ModelProvider:          opts.Model.Provider(),
			ModelID:                opts.Model.ModelID(),
			Settings:               opts.ExperimentalTelemetry,
			Headers:                opts.Headers,
			MaxRetries:             &telemetryMaxRetries,
			ProviderOptions:        providerOptions,
			TimestampGranularities: opts.TimestampGranularities,
		})
		telemetry.FireOnError(prepCtx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, CallID: callID, Error: err})
		return nil, err
	}
	// AudioURL is the TS URL object: providers and download functions get its
	// WHATWG serialization (url.toString()).
	if opts.AudioURL != "" {
		if href, err := urlnorm.Href(opts.AudioURL); err == nil {
			opts.AudioURL = href
		}
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
	// TS 36802d48af: an http(s) URL goes to a model with native URL
	// transcription as is, unless the caller supplied a download function.
	urlModel, nativeCapable := opts.Model.(provider.TranscriptionURLModel)
	nativeURL := len(audio) == 0 && opts.AudioURL != "" && nativeCapable &&
		opts.Download == nil && opts.DownloadWithMetadata == nil && isHTTPAudioURL(opts.AudioURL)
	if len(audio) == 0 && opts.AudioURL != "" && !nativeURL {
		data, mediaType, err := downloadTranscriptionAudio(ctx, opts)
		if err != nil {
			return failPreparation(err)
		}
		audio, downloadedMediaType = data, mediaType
	}
	// TS does not reject empty audio; only a missing source is an error.
	if audio == nil && opts.Audio == nil && opts.AudioBase64 == "" && opts.AudioURL == "" {
		return nil, fmt.Errorf("audio is required")
	}
	resolveMediaType := func(data []byte, downloaded string) string {
		if opts.MimeType != "" {
			return opts.MimeType
		}
		if mediaType := getAudioMediaType(downloaded); mediaType != "" {
			return mediaType
		}
		return detectTranscriptionMediaType(data)
	}
	// The native URL path has no audio size or media type (TS undefined).
	var mediaType string
	var audioByteLength *int64
	if !nativeURL {
		mediaType = resolveMediaType(audio, downloadedMediaType)
		n := int64(len(audio))
		audioByteLength = &n
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		CallID:                 callID,
		OperationType:          "ai.transcribe",
		ModelProvider:          opts.Model.Provider(),
		ModelID:                opts.Model.ModelID(),
		Settings:               opts.ExperimentalTelemetry,
		AudioByteLength:        audioByteLength,
		AudioMediaType:         mediaType,
		Headers:                opts.Headers,
		MaxRetries:             &telemetryMaxRetries,
		ProviderOptions:        providerOptions,
		TimestampGranularities: opts.TimestampGranularities,
	})
	headers := transcribeHeadersWithUserAgent(opts.Headers)
	callOptions := &provider.TranscriptionOptions{
		Audio:                  audio,
		MimeType:               mediaType,
		ProviderOptions:        providerOptions,
		Headers:                headers,
		TimestampGranularities: opts.TimestampGranularities,
	}
	transcribeOnce := func(callCtx context.Context) (*types.TranscriptionResult, error) {
		if nativeURL {
			result, err := urlModel.DoTranscribeFromURL(callCtx, &provider.TranscriptionURLOptions{
				AudioURL:               opts.AudioURL,
				ProviderOptions:        providerOptions,
				Headers:                headers,
				TimestampGranularities: opts.TimestampGranularities,
			})
			if err != nil || result != nil {
				return result, err
			}
			// The model declined this URL (for example a redirect): keep
			// the validated download and the file path, as TS does.
			data, downloaded, err := downloadTranscriptionAudio(callCtx, TranscribeOptions{AudioURL: opts.AudioURL})
			if err != nil {
				return nil, err
			}
			nativeURL = false
			audio = data
			mediaType = resolveMediaType(audio, downloaded)
			n := int64(len(audio))
			audioByteLength = &n
			callOptions.Audio, callOptions.MimeType = audio, mediaType
		}
		return opts.Model.DoTranscribe(callCtx, callOptions)
	}
	maxRetries := preparedMaxRetries(opts.MaxRetries)
	var raw *types.TranscriptionResult
	var err error
	if maxRetries == 0 {
		raw, err = transcribeOnce(ctx)
	} else {
		err = retryutil.Do(ctx, retryutil.Config{
			MaxRetries:   maxRetries,
			InitialDelay: 2 * time.Second,
			Multiplier:   2,
			Jitter:       false,
			ShouldRetry:  isGatewayCallRetryable,
		}, func(retryCtx context.Context) error {
			var transcribeErr error
			raw, transcribeErr = transcribeOnce(retryCtx)
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
	// TS TranscriptionEndEvent: segments, words, language,
	// durationInSeconds, warnings and response beside the text.
	telemetrySegments := make([]telemetry.TelemetryTranscriptTiming, len(segments))
	for i, seg := range segments {
		telemetrySegments[i] = telemetry.TelemetryTranscriptTiming{Text: seg.Text, StartSecond: seg.StartSecond, EndSecond: seg.EndSecond}
	}
	var telemetryWords []telemetry.TelemetryTranscriptTiming
	if raw.Words != nil {
		telemetryWords = make([]telemetry.TelemetryTranscriptTiming, len(raw.Words))
		for i, w := range raw.Words {
			telemetryWords[i] = telemetry.TelemetryTranscriptTiming{Text: w.Text, StartSecond: w.StartSecond, EndSecond: w.EndSecond}
		}
	}
	responseMeta := responses[0]
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		CallID:          callID,
		OperationType:   "ai.transcribe",
		ModelProvider:   opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		Settings:        opts.ExperimentalTelemetry,
		Text:            raw.Text,
		AudioByteLength: audioByteLength,
		AudioMediaType:  mediaType,
		// TS endEvent providerMetadata: result.providerMetadata, undefined
		// when the model reported none (only the result defaults to {}).
		ProviderMetadata:  raw.ProviderMetadata,
		ProviderUsage:     raw.ProviderUsage,
		Segments:          telemetrySegments,
		Words:             telemetryWords,
		Language:          raw.Language,
		DurationInSeconds: raw.DurationInSeconds,
		Warnings:          warnings,
		Response: &telemetry.TelemetryAudioResponse{
			Timestamp: responseMeta.Timestamp,
			ModelID:   responseMeta.ModelID,
			Headers:   responseMeta.Headers,
			Body:      responseMeta.Body,
		},
	})
	return &TranscribeResult{
		Text:              raw.Text,
		Segments:          segments,
		Words:             raw.Words,
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

// downloadTranscriptionAudio fetches opts.AudioURL with opts.Download, else
// opts.DownloadWithMetadata, else the validated default download (2 GiB),
// returning the bytes and any response media type.
func downloadTranscriptionAudio(ctx context.Context, opts TranscribeOptions) ([]byte, string, error) {
	if opts.Download != nil {
		data, err := opts.Download(ctx, opts.AudioURL)
		return data, "", err
	}
	download := opts.DownloadWithMetadata
	if download == nil {
		download = CreateURLDownloadWithMetadata(nil)
	}
	result, err := download(ctx, opts.AudioURL)
	if err != nil || result == nil {
		return nil, "", err
	}
	return result.Data, result.MediaType, nil
}

// isHTTPAudioURL reports whether raw is an absolute http: or https: URL
// (TS `audio.protocol === 'https:' || audio.protocol === 'http:'`).
func isHTTPAudioURL(raw string) bool {
	u, err := urlnorm.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// getAudioMediaType is TS getAudioMediaType (5d18351e1e): a downloaded
// Content-Type is used only when, lower-cased and without parameters, it is
// a full audio/<subtype> media type; generic types such as
// application/octet-stream return "" so the bytes are sniffed instead.
func getAudioMediaType(contentType string) string {
	if contentType == "" {
		return ""
	}
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.ToLower(providerutils.JSTrim(mediaType))
	if fileutil.TopLevelMediaType(mediaType) == "audio" && fileutil.IsFullMediaType(mediaType) {
		return mediaType
	}
	return ""
}

func transcribeHeadersWithUserAgent(headers map[string]string) map[string]string {
	return version.WithUserAgentSuffix(headers, version.UserAgent())
}

// decodeTranscriptionAudioBase64 is TS convertDataContentToUint8Array for a
// string: convertBase64ToUint8Array (base64url mapped, then atob's forgiving
// decode: ASCII whitespace ignored, padding optional).
func decodeTranscriptionAudioBase64(content string) ([]byte, error) {
	if decoded, err := fileutil.ConvertBase64ToBytes(content); err == nil {
		return decoded, nil
	}
	return nil, &providererrors.InvalidArgumentError{
		Field:   "audio",
		Message: "invalid base64 audio data",
	}
}
