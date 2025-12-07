package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	return "v3"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "openai"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	// Use internal http client's Do method with custom headers
	req := internalhttp.Request{
		Method: "POST",
		Path:   "/v1/audio/transcriptions",
		Body:   body,
		Headers: map[string]string{
			"Content-Type": contentType,
		},
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		return nil, providererrors.NewProviderError("openai", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenAI Whisper API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var response openaiTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode transcription response: %w", err)
	}

	return &types.TranscriptionResult{
		Text: response.Text,
		Usage: types.TranscriptionUsage{
			DurationSeconds: response.Duration,
		},
	}, nil
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

	// Add response format
	responseFormat := "json"
	if opts.Timestamps {
		responseFormat = "verbose_json"
	}
	if err := writer.WriteField("response_format", responseFormat); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
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
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
}
