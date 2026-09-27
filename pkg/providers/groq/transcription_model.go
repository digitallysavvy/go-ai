package groq

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
// Groq's Whisper-based batch transcription (POST /audio/transcriptions).
// Mirrors TS GroqTranscriptionModel; streaming transcription is out of scope.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Groq transcription model.
func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "groq" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// groqTranscriptionOptions mirrors groqTranscriptionModelOptions in TS.
type groqTranscriptionOptions struct {
	Language               string
	Prompt                 string
	ResponseFormat         string
	Temperature            *float64
	TimestampGranularities []string
}

func resolveGroqTranscriptionOptions(providerOptions map[string]interface{}) groqTranscriptionOptions {
	raw, _ := providerOptions["groq"].(map[string]interface{})
	var opts groqTranscriptionOptions
	if raw == nil {
		return opts
	}
	if v, ok := raw["language"].(string); ok {
		opts.Language = v
	}
	if v, ok := raw["prompt"].(string); ok {
		opts.Prompt = v
	}
	if v, ok := raw["responseFormat"].(string); ok {
		opts.ResponseFormat = v
	}
	switch v := raw["temperature"].(type) {
	case float64:
		opts.Temperature = &v
	case int:
		f := float64(v)
		opts.Temperature = &f
	}
	if arr, ok := raw["timestampGranularities"].([]interface{}); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				opts.TimestampGranularities = append(opts.TimestampGranularities, s)
			}
		}
	} else if arr, ok := raw["timestampGranularities"].([]string); ok {
		opts.TimestampGranularities = arr
	}
	return opts
}

// DoTranscribe performs batch speech-to-text transcription.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	audio, err := groqAudioBytes(opts)
	if err != nil {
		return nil, err
	}
	groqOpts := resolveGroqTranscriptionOptions(opts.ProviderOptions)

	body, contentType, err := m.buildMultipartBody(audio, opts.MimeType, groqOpts)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/audio/transcriptions",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("groq", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, providererrors.NewProviderError("groq", resp.StatusCode, "", string(resp.Body), nil)
	}

	timestamp := time.Now()
	responseHeaders := providerutils.ExtractHeaders(resp.Headers)

	// A plain-text response (responseFormat: "text") is not JSON; only try
	// the JSON path when the response doesn't look like bare text.
	if groqOpts.ResponseFormat == "text" {
		text := string(resp.Body)
		return &types.TranscriptionResult{
			Text: text,
			Response: &types.ResponseMetadata{
				Timestamp: timestamp,
				ModelID:   m.modelID,
				Headers:   responseHeaders,
				Body:      text,
			},
		}, nil
	}

	var response groqTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("groq: failed to decode transcription response: %w", err)
	}

	result := &types.TranscriptionResult{
		Text:     response.Text,
		Language: response.Language,
	}
	if response.Duration != nil {
		result.DurationInSeconds = response.Duration
	}

	switch {
	case len(response.Segments) > 0:
		for _, seg := range response.Segments {
			result.Segments = append(result.Segments, types.TranscriptionTimestamp{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			})
		}
	case len(response.Words) > 0:
		// Fall back to word-level timestamps when segment timestamps aren't
		// available (TS: response.segments?.map(...) ?? response.words?.map(...)).
		for _, word := range response.Words {
			result.Segments = append(result.Segments, types.TranscriptionTimestamp{
				Text:  word.Word,
				Start: word.Start,
				End:   word.End,
			})
		}
	}

	result.Response = &types.ResponseMetadata{
		Timestamp: timestamp,
		ModelID:   m.modelID,
		Headers:   responseHeaders,
		Body:      response,
	}
	return result, nil
}

func groqAudioBytes(opts *provider.TranscriptionOptions) ([]byte, error) {
	if len(opts.Audio) > 0 {
		return opts.Audio, nil
	}
	if opts.AudioBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.AudioBase64)
		if err != nil {
			return nil, fmt.Errorf("groq: failed to decode base64 audio: %w", err)
		}
		return decoded, nil
	}
	return nil, fmt.Errorf("groq: no audio data provided")
}

func (m *TranscriptionModel) buildMultipartBody(audio []byte, mediaType string, opts groqTranscriptionOptions) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", err
	}

	part, err := writer.CreateFormFile("file", "audio."+groqAudioExtension(mediaType))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}

	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, "", err
		}
	}
	if opts.Prompt != "" {
		if err := writer.WriteField("prompt", opts.Prompt); err != nil {
			return nil, "", err
		}
	}
	if opts.ResponseFormat != "" {
		if err := writer.WriteField("response_format", opts.ResponseFormat); err != nil {
			return nil, "", err
		}
	}
	if opts.Temperature != nil {
		if err := writer.WriteField("temperature", strconv.FormatFloat(*opts.Temperature, 'f', -1, 64)); err != nil {
			return nil, "", err
		}
	}
	for _, granularity := range opts.TimestampGranularities {
		if err := writer.WriteField("timestamp_granularities[]", granularity); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func groqAudioExtension(mediaType string) string {
	switch mediaType {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/flac":
		return "flac"
	case "audio/ogg":
		return "ogg"
	default:
		return "wav"
	}
}

// groqTranscriptionResponse mirrors groqTranscriptionResponseSchema in TS.
// Additional fields (task, language, duration, segments, words) are only
// present when response_format is verbose_json.
type groqTranscriptionResponse struct {
	Text     string   `json:"text"`
	Task     string   `json:"task,omitempty"`
	Language string   `json:"language,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Segments []struct {
		Text  string  `json:"text"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"segments,omitempty"`
	Words []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words,omitempty"`
	XGroq struct {
		ID string `json:"id"`
	} `json:"x_groq,omitempty"`
}
