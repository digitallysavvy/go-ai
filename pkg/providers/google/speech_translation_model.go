package google

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

// liveSpeechTranslationWebSocketPath is the Live API bidi service path for
// streaming speech translation, mirroring TS's liveWebSocketPath in
// google-speech-translation-model.ts. It is the same BidiGenerateContent
// endpoint used for streaming transcription (transcription_stream.go);
// `translationConfig` in the setup message is what switches the mode.
const liveSpeechTranslationWebSocketPath = "google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

// speechTranslationDefaultFinishGraceDuration is the quiet window after the
// input audio ends during which trailing output is still accepted before the
// stream finishes with no terminal signal from the server (TS
// defaultFinishGraceMs = 1000; Live Translation is continuous and never
// emits turnComplete on its own).
const speechTranslationDefaultFinishGraceDuration = 1 * time.Second

// googleLiveOutputAudioRate is the fixed sample rate of Gemini Live's output
// audio (TS googleLiveOutputAudioRate), used to convert decoded PCM16 sample
// counts into a duration for trailing-silence detection.
const googleLiveOutputAudioRate = 24000

// pcm16SilenceAmplitudeThreshold is the maximum absolute PCM16 sample value
// still considered silence (TS pcm16SilenceAmplitudeThreshold).
const pcm16SilenceAmplitudeThreshold = 128

// SpeechTranslationModel implements provider.SpeechTranslationModel over the
// Gemini Live API WebSocket (Live Translation, BidiGenerateContent), TS
// GoogleSpeechTranslationModel (exported there as
// Experimental_GoogleSpeechTranslationModel, with a deprecated
// Experimental_GoogleTranslationModel alias — the Go SDK exposes a single
// name since it has no deprecation-alias convention).
type SpeechTranslationModel struct {
	prov    *Provider
	modelID string

	// finishGraceMs overrides speechTranslationDefaultFinishGraceDuration for
	// tests (mirrors TS config._internal.finishGraceMs). Zero means "use the
	// default".
	finishGraceMs time.Duration
}

// NewSpeechTranslationModel creates a Gemini Live speech translation model.
func NewSpeechTranslationModel(p *Provider, modelID string) *SpeechTranslationModel {
	return &SpeechTranslationModel{prov: p, modelID: modelID}
}

// SpeechTranslationModel returns an experimental streaming speech
// translation model by ID (TS provider.speechTranslationModel).
func (p *Provider) SpeechTranslationModel(modelID string) (provider.SpeechTranslationModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewSpeechTranslationModel(p, modelID), nil
}

// Translation is an alias for SpeechTranslationModel (TS provider.translation).
func (p *Provider) Translation(modelID string) (provider.SpeechTranslationModel, error) {
	return p.SpeechTranslationModel(modelID)
}

func (m *SpeechTranslationModel) SpecificationVersion() string { return "v4" }

func (m *SpeechTranslationModel) Provider() string {
	return m.prov.Name() + ".speech-translation"
}

func (m *SpeechTranslationModel) ModelID() string { return m.modelID }

// speechTranslationModelOptions mirrors TS GoogleSpeechTranslationModelOptions
// (google-speech-translation-model-options.ts).
type speechTranslationModelOptions struct {
	EchoTargetLanguage *bool
}

func parseSpeechTranslationModelOptions(providerOptions map[string]interface{}) speechTranslationModelOptions {
	var out speechTranslationModelOptions
	google, _ := providerOptions["google"].(map[string]interface{})
	if google == nil {
		return out
	}
	if v, ok := google["echoTargetLanguage"].(bool); ok {
		out.EchoTargetLanguage = &v
	}
	return out
}

// validateSpeechTranslationInputAudioFormat mirrors TS
// validateGoogleSpeechTranslationInputAudioFormat: the Gemini Live
// translation API only accepts 16kHz 16-bit PCM input audio.
func validateSpeechTranslationInputAudioFormat(format provider.AudioFormat) error {
	if format.Type != "audio/pcm" || (format.Rate != nil && *format.Rate != 16000) {
		return &providererrors.InvalidArgumentError{
			Field:   "inputAudioFormat",
			Message: "The Gemini Live translation API only supports 16kHz 16-bit PCM input audio.",
		}
	}
	return nil
}

// buildSpeechTranslationSetup builds the Live API `setup` message, mirroring
// TS buildGoogleLiveSpeechTranslationSetup.
func buildSpeechTranslationSetup(modelID, targetLanguage string, opts speechTranslationModelOptions) map[string]interface{} {
	translationConfig := map[string]interface{}{"targetLanguageCode": targetLanguage}
	if opts.EchoTargetLanguage != nil {
		translationConfig["echoTargetLanguage"] = *opts.EchoTargetLanguage
	}
	return map[string]interface{}{
		"model": gemini.GetModelPath(modelID),
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"AUDIO"},
			"translationConfig":  translationConfig,
		},
		"inputAudioTranscription":  map[string]interface{}{},
		"outputAudioTranscription": map[string]interface{}{},
	}
}

// baseSpeechTranslationHeaders returns the provider's default headers
// (x-goog-api-key, configured custom headers, and the `ai-sdk/google/VERSION`
// User-Agent tag), mirroring the Resolvable config.headers TS combines with
// per-call options.headers -- the same tagged getHeaders() closure used for
// REST calls, not a freshly rebuilt untagged header set.
func (m *SpeechTranslationModel) baseSpeechTranslationHeaders() map[string]string {
	return m.prov.client.Headers()
}

// getLiveSpeechTranslationWebSocketURL builds the wss:// Live API URL for
// streaming speech translation, mirroring TS getLiveWebSocketURL (via
// getRealtimeWebSocketURL): the base URL's trailing /v1beta or /v1alpha
// segment is stripped, the scheme is upgraded to ws(s), and the API key
// rides the `key` query parameter.
func getLiveSpeechTranslationWebSocketURL(baseURL, apiKey string) string {
	u := googleRealtimeBaseURL(baseURL)
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/" + liveSpeechTranslationWebSocketPath
	q := u.Query()
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()
	return u.String()
}

// DoStream streams a speech-to-speech translation over the Gemini Live API
// WebSocket. Mirrors TS GoogleSpeechTranslationModel#doStream.
func (m *SpeechTranslationModel) DoStream(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
	if opts == nil {
		opts = &provider.SpeechTranslationStreamOptions{}
	}
	if opts.TargetLanguage == "" {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "targetLanguage",
			Message: fmt.Sprintf("targetLanguage is required for translation model '%s'.", m.modelID),
		}
	}

	translationOpts := parseSpeechTranslationModelOptions(opts.ProviderOptions)

	var warnings []types.Warning

	if err := validateSpeechTranslationInputAudioFormat(opts.InputAudioFormat); err != nil {
		return nil, err
	}

	if opts.SourceLanguage != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "sourceLanguage",
			Details: "The Gemini Live translation API auto-detects the source language and does not accept a source language.",
		})
	}
	if opts.OutputAudioFormat != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputAudioFormat",
			Details: "The Gemini Live API always outputs 24kHz 16-bit PCM audio and does not accept an output audio format.",
		})
	}

	// Resolve the API key from the provider-level and per-call header maps
	// separately, with the per-call value taking precedence, before merging
	// them (see the matching comment in transcription_stream.go DoStream).
	baseAPIKey, filteredBaseHeaders := extractGoogleAPIKeyHeader(m.baseSpeechTranslationHeaders())
	callAPIKey, filteredCallHeaders := extractGoogleAPIKeyHeader(opts.Headers)
	apiKey := baseAPIKey
	if callAPIKey != "" {
		apiKey = callAPIKey
	}
	if apiKey == "" {
		return nil, errors.New("Google Generative AI API key is required for streaming translation.")
	}
	wsHeaders := internalhttp.MergeHeaders(filteredBaseHeaders, filteredCallHeaders)

	setup := buildSpeechTranslationSetup(m.modelID, opts.TargetLanguage, translationOpts)

	inputAudioRate := 16000
	if opts.InputAudioFormat.Rate != nil {
		inputAudioRate = *opts.InputAudioFormat.Rate
	}

	finishGraceMs := m.finishGraceMs
	if finishGraceMs <= 0 {
		finishGraceMs = speechTranslationDefaultFinishGraceDuration
	}

	wsURL := getLiveSpeechTranslationWebSocketURL(m.prov.config.BaseURL, apiKey)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGoogleLiveSpeechTranslationStream(abortCtx, googleLiveSpeechTranslationStreamConfig{
		url:              wsURL,
		headers:          wsHeaders,
		setup:            setup,
		inputAudioRate:   inputAudioRate,
		finishGraceMs:    finishGraceMs,
		warnings:         warnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.SpeechTranslationStreamResult{
		Stream:      stream,
		RequestBody: setup,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

var _ provider.SpeechTranslationModel = (*SpeechTranslationModel)(nil)
