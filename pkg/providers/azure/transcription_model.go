package azure

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

// TranscriptionModel implements the provider.TranscriptionModel interface for Azure OpenAI
type TranscriptionModel struct {
	provider     *Provider
	deploymentID string
}

// NewTranscriptionModel creates a new Azure OpenAI transcription model
func NewTranscriptionModel(provider *Provider, deploymentID string) *TranscriptionModel {
	return &TranscriptionModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "azure-openai"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *TranscriptionModel) ModelID() string {
	return m.deploymentID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	// Azure OpenAI transcription endpoint
	path := fmt.Sprintf("/openai/deployments/%s/audio/transcriptions?api-version=%s",
		m.deploymentID,
		m.provider.APIVersion())

	// Use internal http client's Do method with custom headers
	req := internalhttp.Request{
		Method: "POST",
		Path:   path,
		Body:   body,
		Headers: map[string]string{
			"Content-Type": contentType,
		},
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		return nil, providererrors.NewProviderError("azure-openai", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Azure OpenAI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body, opts.Timestamps)
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add audio file
	part, err := writer.CreateFormFile("file", "audio.mp3")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(opts.Audio); err != nil {
		return nil, "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add optional parameters
	if opts.Language != "" {
		writer.WriteField("language", opts.Language)
	}

	// Set response format
	responseFormat := "json"
	if opts.Timestamps {
		responseFormat = "verbose_json"
	}
	writer.WriteField("response_format", responseFormat)

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

func (m *TranscriptionModel) convertResponse(body []byte, timestamps bool) (*types.TranscriptionResult, error) {
	if timestamps {
		// Verbose JSON format with timestamps
		var response struct {
			Text     string  `json:"text"`
			Language string  `json:"language"`
			Duration float64 `json:"duration"`
			Segments []struct {
				ID               int     `json:"id"`
				Seek             int     `json:"seek"`
				Start            float64 `json:"start"`
				End              float64 `json:"end"`
				Text             string  `json:"text"`
				Tokens           []int   `json:"tokens"`
				Temperature      float64 `json:"temperature"`
				AvgLogprob       float64 `json:"avg_logprob"`
				CompressionRatio float64 `json:"compression_ratio"`
				NoSpeechProb     float64 `json:"no_speech_prob"`
			} `json:"segments"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		timestampList := make([]types.TranscriptionTimestamp, len(response.Segments))
		for i, seg := range response.Segments {
			timestampList[i] = types.TranscriptionTimestamp{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			}
		}

		return &types.TranscriptionResult{
			Text:       response.Text,
			Timestamps: timestampList,
			Usage: types.TranscriptionUsage{
				DurationSeconds: response.Duration,
			},
		}, nil
	}

	// Simple JSON format
	var response struct {
		Text string `json:"text"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &types.TranscriptionResult{
		Text:       response.Text,
		Timestamps: nil,
		Usage:      types.TranscriptionUsage{},
	}, nil
}
