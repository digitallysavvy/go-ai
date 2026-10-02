package mistral

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for
// Mistral's Voxtral batch transcription models (POST
// /v1/audio/transcriptions). Only batch transcription is supported: Mistral
// (like the TS SDK's MistralTranscriptionModel) has no streaming
// transcription endpoint.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Mistral transcription model.
func NewTranscriptionModel(provider *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "mistral" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// DoTranscribe performs batch speech-to-text transcription.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	currentDate := time.Now()

	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/v1/audio/transcriptions",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
	}

	var response mistralTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("mistral: failed to decode transcription response: %w", err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(response.Segments))
	for _, seg := range response.Segments {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  seg.Text,
			Start: seg.Start,
			End:   seg.End,
		})
	}

	var durationInSeconds *float64
	if response.Usage != nil && response.Usage.PromptAudioSeconds != nil {
		durationInSeconds = response.Usage.PromptAudioSeconds
	} else if len(segments) > 0 {
		v := segments[len(segments)-1].End
		durationInSeconds = &v
	}
	var duration float64
	if durationInSeconds != nil {
		duration = *durationInSeconds
	}

	result := &types.TranscriptionResult{
		Text:              response.Text,
		Segments:          segments,
		Language:          response.Language,
		DurationInSeconds: durationInSeconds,
		Usage:             types.TranscriptionUsage{DurationSeconds: duration},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   response.Model,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}

	if metadata := mistralTranscriptionProviderMetadata(response); metadata != nil {
		result.ProviderMetadata = map[string]interface{}{"mistral": metadata}
	}

	// TS: `...(response.usage != null && { usage: response.usage })` — the
	// raw wire-format usage object (snake_case keys), not the camelCased
	// providerMetadata.mistral.usage transform above.
	if response.Usage != nil {
		rawUsage := map[string]interface{}{}
		if response.Usage.PromptTokens != nil {
			rawUsage["prompt_tokens"] = *response.Usage.PromptTokens
		}
		if response.Usage.CompletionTokens != nil {
			rawUsage["completion_tokens"] = *response.Usage.CompletionTokens
		}
		if response.Usage.TotalTokens != nil {
			rawUsage["total_tokens"] = *response.Usage.TotalTokens
		}
		if response.Usage.PromptAudioSeconds != nil {
			rawUsage["prompt_audio_seconds"] = *response.Usage.PromptAudioSeconds
		}
		if response.Usage.RequestCount != nil {
			rawUsage["request_count"] = *response.Usage.RequestCount
		}
		result.ProviderUsage = rawUsage
	}

	return result, nil
}

// mistralTranscriptionProviderMetadata builds the providerMetadata.mistral
// object mirroring TS: a "usage" object (camelCase field names) when usage is
// present, and a "segments" array (only entries carrying type/score/speakerId)
// when any such segment exists.
func mistralTranscriptionProviderMetadata(response mistralTranscriptionResponse) map[string]interface{} {
	metadata := map[string]interface{}{}

	if response.Usage != nil {
		usage := map[string]interface{}{}
		if response.Usage.PromptTokens != nil {
			usage["promptTokens"] = *response.Usage.PromptTokens
		}
		if response.Usage.CompletionTokens != nil {
			usage["completionTokens"] = *response.Usage.CompletionTokens
		}
		if response.Usage.TotalTokens != nil {
			usage["totalTokens"] = *response.Usage.TotalTokens
		}
		if response.Usage.PromptAudioSeconds != nil {
			usage["promptAudioSeconds"] = *response.Usage.PromptAudioSeconds
		}
		if response.Usage.RequestCount != nil {
			usage["requestCount"] = *response.Usage.RequestCount
		}
		if len(usage) > 0 {
			metadata["usage"] = usage
		}
	}

	var providerSegments []map[string]interface{}
	for _, seg := range response.Segments {
		if seg.Type == "" && seg.Score == nil && seg.SpeakerID == "" {
			continue
		}
		ps := map[string]interface{}{
			"text":        seg.Text,
			"startSecond": seg.Start,
			"endSecond":   seg.End,
		}
		if seg.Type != "" {
			ps["type"] = seg.Type
		}
		if seg.Score != nil {
			ps["score"] = *seg.Score
		}
		if seg.SpeakerID != "" {
			ps["speakerId"] = seg.SpeakerID
		}
		providerSegments = append(providerSegments, ps)
	}
	if len(providerSegments) > 0 {
		metadata["segments"] = providerSegments
	}

	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// mistralTranscriptionOptions mirrors mistralTranscriptionModelOptions in
// ai/packages/mistral/src/mistral-transcription-model-options.ts.
type mistralTranscriptionOptions struct {
	Language               string
	Temperature            *float64
	TimestampGranularities []string
	Diarize                *bool
	ContextBias            []string
}

// extractMistralTranscriptionOptions reads providerOptions.mistral. Mistral
// documents language and timestampGranularities as mutually exclusive;
// rejecting the combination locally provides a stable SDK error instead of an
// API 4xx, matching TS.
func extractMistralTranscriptionOptions(opts *provider.TranscriptionOptions) (*mistralTranscriptionOptions, error) {
	if opts == nil || opts.ProviderOptions == nil {
		return nil, nil
	}
	raw, ok := opts.ProviderOptions["mistral"]
	if !ok || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid mistral provider options: expected object")
	}

	result := &mistralTranscriptionOptions{}
	if v, ok := m["language"].(string); ok {
		result.Language = v
	}
	if v, ok := m["temperature"].(float64); ok {
		result.Temperature = &v
	}
	if items, ok := m["timestampGranularities"].([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	if v, ok := m["diarize"].(bool); ok {
		result.Diarize = &v
	}
	if items, ok := m["contextBias"].([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				result.ContextBias = append(result.ContextBias, s)
			}
		}
	}

	if result.Language != "" && len(result.TimestampGranularities) > 0 {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "providerOptions.mistral.language cannot be combined with providerOptions.mistral.timestampGranularities",
		}
	}

	return result, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (io.Reader, string, error) {
	mistralOpts, err := extractMistralTranscriptionOptions(opts)
	if err != nil {
		return nil, "", err
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", err
	}

	audioBytes := opts.Audio
	if len(audioBytes) == 0 && opts.AudioBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.AudioBase64)
		if err != nil {
			return nil, "", fmt.Errorf("mistral: failed to decode base64 audio: %w", err)
		}
		audioBytes = decoded
	}

	part, err := writer.CreateFormFile("file", "audio."+mistralAudioExtension(opts.MimeType))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return nil, "", fmt.Errorf("failed to write audio data: %w", err)
	}

	language := ""
	if mistralOpts != nil && mistralOpts.Language != "" {
		language = mistralOpts.Language
	} else if opts.Language != "" {
		language = opts.Language
	}
	if language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return nil, "", err
		}
	}

	if mistralOpts != nil {
		if mistralOpts.Temperature != nil {
			if err := writer.WriteField("temperature", strconv.FormatFloat(*mistralOpts.Temperature, 'f', -1, 64)); err != nil {
				return nil, "", err
			}
		}
		for _, g := range mistralOpts.TimestampGranularities {
			if err := writer.WriteField("timestamp_granularities", g); err != nil {
				return nil, "", err
			}
		}
		if mistralOpts.Diarize != nil {
			if err := writer.WriteField("diarize", strconv.FormatBool(*mistralOpts.Diarize)); err != nil {
				return nil, "", err
			}
		}
		for _, cb := range mistralOpts.ContextBias {
			if err := writer.WriteField("context_bias", cb); err != nil {
				return nil, "", err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

func mistralAudioExtension(mimeType string) string {
	switch mimeType {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "m4a"
	case "audio/flac", "audio/x-flac":
		return "flac"
	case "audio/ogg":
		return "ogg"
	default:
		return "audio"
	}
}

type mistralTranscriptionResponse struct {
	Model    string `json:"model"`
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
	Segments []struct {
		Type      string   `json:"type,omitempty"`
		Text      string   `json:"text"`
		Start     float64  `json:"start"`
		End       float64  `json:"end"`
		Score     *float64 `json:"score,omitempty"`
		SpeakerID string   `json:"speaker_id,omitempty"`
	} `json:"segments,omitempty"`
	Usage *struct {
		PromptTokens       *int     `json:"prompt_tokens,omitempty"`
		CompletionTokens   *int     `json:"completion_tokens,omitempty"`
		TotalTokens        *int     `json:"total_tokens,omitempty"`
		PromptAudioSeconds *float64 `json:"prompt_audio_seconds,omitempty"`
		RequestCount       *int     `json:"request_count,omitempty"`
	} `json:"usage,omitempty"`
}
