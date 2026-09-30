package openai

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechTranslationModel implements provider.SpeechTranslationModel over the
// OpenAI realtime translations WebSocket, TS OpenAISpeechTranslationModel.
type SpeechTranslationModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechTranslationModel creates an OpenAI realtime speech translation
// model.
func NewSpeechTranslationModel(p *Provider, modelID string) *SpeechTranslationModel {
	return &SpeechTranslationModel{provider: p, modelID: modelID}
}

// SpeechTranslationModel returns an experimental streaming speech
// translation model by ID (TS provider.speechTranslationModel).
func (p *Provider) SpeechTranslationModel(modelID string) (provider.SpeechTranslationModel, error) {
	if modelID == "" {
		modelID = "gpt-realtime-translate"
	}
	return NewSpeechTranslationModel(p, modelID), nil
}

// Translation is an alias for SpeechTranslationModel (TS provider.translation).
func (p *Provider) Translation(modelID string) (provider.SpeechTranslationModel, error) {
	return p.SpeechTranslationModel(modelID)
}

func (m *SpeechTranslationModel) SpecificationVersion() string { return "v4" }

func (m *SpeechTranslationModel) Provider() string {
	return m.provider.Name() + ".speech-translation"
}

func (m *SpeechTranslationModel) ModelID() string { return m.modelID }

// validateOpenAISpeechTranslationInputAudioFormat mirrors TS
// validateOpenAISpeechTranslationInputAudioFormat: the OpenAI Realtime
// translation API only accepts 24kHz 16-bit PCM input audio.
func validateOpenAISpeechTranslationInputAudioFormat(format provider.AudioFormat) error {
	if format.Type != "audio/pcm" || (format.Rate != nil && *format.Rate != 24000) {
		return &providererrors.InvalidArgumentError{
			Field:   "inputAudioFormat",
			Message: "The OpenAI Realtime translation API only supports 24kHz 16-bit PCM input audio.",
		}
	}
	return nil
}

// buildOpenAISpeechTranslationSession mirrors TS
// buildOpenAIRealtimeSpeechTranslationSession.
func buildOpenAISpeechTranslationSession(targetLanguage string) map[string]interface{} {
	return map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"audio": map[string]interface{}{
				"input": map[string]interface{}{
					"transcription":   map[string]interface{}{"model": "gpt-realtime-whisper"},
					"noise_reduction": nil,
				},
				"output": map[string]interface{}{"language": targetLanguage},
			},
		},
	}
}

// realtimeSpeechTranslationWebSocketURL builds the wss:// realtime
// translations URL, mirroring TS `this.config.url({ path:
// '/realtime/translations?model=...' })`.
func realtimeSpeechTranslationWebSocketURL(baseURL, modelID string) string {
	wsBase := strings.Replace(baseURL, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	return wsBase + "/realtime/translations?model=" + url.QueryEscape(modelID)
}

// baseSpeechTranslationWSHeaders rebuilds the provider's default headers
// (Authorization, organization, project, custom config headers) for the
// WebSocket handshake, mirroring baseWSHeaders in transcription_stream.go.
func (m *SpeechTranslationModel) baseSpeechTranslationWSHeaders() map[string]string {
	cfg := m.provider.config
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	if cfg.Organization != "" {
		headers["OpenAI-Organization"] = cfg.Organization
	}
	if cfg.Project != "" {
		headers["OpenAI-Project"] = cfg.Project
	}
	return internalhttp.MergeHeaders(headers, cfg.Headers)
}

// DoStream streams a speech-to-speech translation over the OpenAI realtime
// translations WebSocket. Mirrors TS OpenAISpeechTranslationModel#doStream.
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

	if err := validateOpenAISpeechTranslationInputAudioFormat(opts.InputAudioFormat); err != nil {
		return nil, err
	}

	var warnings []types.Warning
	if opts.SourceLanguage != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "sourceLanguage",
			Details: "The OpenAI Realtime translation API auto-detects the source language and does not accept a source language.",
		})
	}
	if opts.OutputAudioFormat != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputAudioFormat",
			Details: "The OpenAI Realtime translation API always outputs 24kHz 16-bit PCM audio and does not accept an output audio format.",
		})
	}

	headers := internalhttp.MergeHeaders(m.baseSpeechTranslationWSHeaders(), opts.Headers)
	sessionUpdate := buildOpenAISpeechTranslationSession(opts.TargetLanguage)
	wsURL := realtimeSpeechTranslationWebSocketURL(m.provider.config.BaseURL, m.modelID)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newOpenAIRealtimeSpeechTranslationStream(abortCtx, openAIRealtimeSpeechTranslationStreamConfig{
		url:              wsURL,
		headers:          headers,
		sessionUpdate:    sessionUpdate,
		warnings:         warnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.SpeechTranslationStreamResult{
		Stream:      stream,
		RequestBody: sessionUpdate,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

var _ provider.SpeechTranslationModel = (*SpeechTranslationModel)(nil)
