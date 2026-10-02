package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for OpenAI Whisper
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new OpenAI transcription model
func NewTranscriptionModel(provider *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return m.provider.Name()
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if isRealtimeTranscriptionModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("non-streaming transcription with %s", m.modelID),
		}
	}

	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	// Use internal http client's Do method with custom headers
	req := internalhttp.Request{
		Method:  "POST",
		Path:    "/audio/transcriptions",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenAI Whisper API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var response openaiTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode transcription response: %w", err)
	}

	result := &types.TranscriptionResult{
		Text: response.Text,
		Usage: types.TranscriptionUsage{
			DurationSeconds: response.Duration,
		},
		ProviderUsage: response.Usage,
	}

	// Timestamped segments/words (abb9ebf: diarized segments carry an extra
	// "speaker" field, surfaced separately via ProviderMetadata below).
	if len(response.Segments) > 0 {
		result.Segments = make([]types.TranscriptionTimestamp, len(response.Segments))
		for i, seg := range response.Segments {
			result.Segments[i] = types.TranscriptionTimestamp{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			}
		}
	} else if len(response.Words) > 0 {
		result.Segments = make([]types.TranscriptionTimestamp, len(response.Words))
		for i, w := range response.Words {
			result.Segments[i] = types.TranscriptionTimestamp{
				Text:  w.Word,
				Start: w.Start,
				End:   w.End,
			}
		}
	}
	result.Timestamps = result.Segments

	// diarized_json (gpt-4o-transcribe-diarize): segments carry a "speaker"
	// field. Surface them separately via providerMetadata.openai.segments
	// (abb9ebf), matching TS's diarizedSegments.
	var diarizedSegments []map[string]interface{}
	for _, seg := range response.Segments {
		if seg.Speaker == "" {
			continue
		}
		diarizedSegments = append(diarizedSegments, map[string]interface{}{
			"text":        seg.Text,
			"startSecond": seg.Start,
			"endSecond":   seg.End,
			"speaker":     seg.Speaker,
		})
	}
	if len(diarizedSegments) > 0 {
		result.ProviderMetadata = map[string]interface{}{
			"openai": map[string]interface{}{
				"segments": diarizedSegments,
			},
		}
	}

	return result, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", "audio."+getExtensionFromMimeType(opts.MimeType))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(opts.Audio); err != nil {
		return nil, "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model
	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", err
	}

	// Add language if specified
	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, "", err
		}
	}

	// Add timestamp granularities if requested
	if opts.Timestamps {
		if err := writer.WriteField("timestamp_granularities[]", "word"); err != nil {
			return nil, "", err
		}
	}

	openaiOpts := transcriptionOpenAIOptions(opts.ProviderOptions)
	isDiarizationModel := m.modelID == "gpt-4o-transcribe-diarize"
	isGpt4oTranscribeModel := m.modelID == "gpt-4o-transcribe" || m.modelID == "gpt-4o-mini-transcribe"

	// Add response format (TS getArgs: whisper-1 unconditionally gets
	// "verbose_json" -- set before, and never overridden by,
	// providerOptions.openai.responseFormat. Every other model defaults to
	// diarized_json for the diarize model, json for gpt-4o(-mini)-transcribe,
	// verbose_json otherwise, with an explicit responseFormat override
	// winning over all of those; abb9ebf).
	responseFormat := "verbose_json"
	if m.modelID != "whisper-1" {
		switch {
		case openaiOpts.ResponseFormat != "":
			responseFormat = openaiOpts.ResponseFormat
		case isDiarizationModel:
			responseFormat = "diarized_json"
		case isGpt4oTranscribeModel:
			responseFormat = "json"
		default:
			responseFormat = "verbose_json"
		}
	}
	if err := writer.WriteField("response_format", responseFormat); err != nil {
		return nil, "", err
	}

	if openaiOpts.Prompt != "" {
		if err := writer.WriteField("prompt", openaiOpts.Prompt); err != nil {
			return nil, "", err
		}
	}
	if openaiOpts.HasTemperature {
		if err := writer.WriteField("temperature", formatFloat(openaiOpts.Temperature)); err != nil {
			return nil, "", err
		}
	}
	for _, inc := range openaiOpts.Include {
		if err := writer.WriteField("include[]", inc); err != nil {
			return nil, "", err
		}
	}
	for _, tg := range openaiOpts.TimestampGranularities {
		if err := writer.WriteField("timestamp_granularities[]", tg); err != nil {
			return nil, "", err
		}
	}

	// Controls how the audio is split into chunks before transcription
	// (abb9ebf: default "auto" for the diarize model).
	chunkingStrategy := openaiOpts.ChunkingStrategy
	if chunkingStrategy == nil && isDiarizationModel {
		chunkingStrategy = "auto"
	}
	if chunkingStrategy != nil {
		var value string
		switch cs := chunkingStrategy.(type) {
		case string:
			value = cs
		case map[string]interface{}:
			encoded, err := json.Marshal(chunkingStrategyWireFormat(cs))
			if err != nil {
				return nil, "", fmt.Errorf("failed to encode chunkingStrategy: %w", err)
			}
			value = string(encoded)
		}
		if value != "" {
			if err := writer.WriteField("chunking_strategy", value); err != nil {
				return nil, "", err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// chunkingStrategyWireFormat maps camelCase providerOptions keys to the
// snake_case wire format OpenAI expects for the server_vad object form.
func chunkingStrategyWireFormat(cs map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"type": "server_vad"}
	if t, ok := cs["type"]; ok {
		out["type"] = t
	}
	if v, ok := cs["threshold"]; ok {
		out["threshold"] = v
	}
	if v, ok := cs["prefixPaddingMs"]; ok {
		out["prefix_padding_ms"] = v
	}
	if v, ok := cs["silenceDurationMs"]; ok {
		out["silence_duration_ms"] = v
	}
	return out
}

func formatFloat(f float64) string {
	encoded, _ := json.Marshal(f)
	return string(encoded)
}

// transcriptionOpenAIOptionsValue holds providerOptions.openai transcription
// settings (abb9ebf: openai-transcription-model-options.ts).
type transcriptionOpenAIOptionsValue struct {
	ResponseFormat         string
	ChunkingStrategy       interface{} // string ("auto") or map[string]interface{} (server_vad)
	Prompt                 string
	HasTemperature         bool
	Temperature            float64
	Include                []string
	TimestampGranularities []string
}

func transcriptionOpenAIOptions(providerOptions map[string]interface{}) transcriptionOpenAIOptionsValue {
	var result transcriptionOpenAIOptionsValue
	if providerOptions == nil {
		return result
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return result
	}
	if v, ok := openaiOpts["responseFormat"].(string); ok {
		result.ResponseFormat = v
	}
	if v, ok := openaiOpts["chunkingStrategy"]; ok {
		result.ChunkingStrategy = v
	}
	if v, ok := openaiOpts["prompt"].(string); ok {
		result.Prompt = v
	}
	if v, ok := speechSpeedOption(openaiOpts["temperature"]); ok {
		result.Temperature = v
		result.HasTemperature = true
	}
	if v, ok := openaiOpts["include"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.Include = append(result.Include, s)
			}
		}
	}
	if v, ok := openaiOpts["timestampGranularities"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	return result
}

func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	default:
		return "audio"
	}
}

type openaiTranscriptionResponse struct {
	Text     string                 `json:"text"`
	Duration float64                `json:"duration"`
	Language string                 `json:"language,omitempty"`
	Usage    map[string]interface{} `json:"usage,omitempty"`
	Segments []struct {
		Text    string  `json:"text"`
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Speaker string  `json:"speaker,omitempty"`
	} `json:"segments,omitempty"`
	Words []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words,omitempty"`
}
