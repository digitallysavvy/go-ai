package gladia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscriptionModel represents a Gladia transcription model
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v1"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "gladia"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// gladiaTranscriptionRequest represents the Gladia API request
type gladiaTranscriptionRequest struct {
	Audio    string `json:"audio"`
	Language string `json:"language,omitempty"`
}

// gladiaTranscriptionResponse represents the Gladia API response
type gladiaTranscriptionResponse struct {
	Result struct {
		Transcription struct {
			FullTranscript string `json:"full_transcript"`
			Utterances     []struct {
				Text  string  `json:"text"`
				Start float64 `json:"start"`
				End   float64 `json:"end"`
			} `json:"utterances"`
		} `json:"transcription"`
	} `json:"result"`
	Metadata struct {
		Duration float64 `json:"duration"`
	} `json:"metadata"`
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add audio file
	part, err := writer.CreateFormField("audio")
	if err != nil {
		return nil, fmt.Errorf("failed to create form field: %w", err)
	}
	if _, err := part.Write(opts.Audio); err != nil {
		return nil, fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add optional language
	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, fmt.Errorf("failed to write language field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/audio/transcription", m.provider.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-gladia-key", m.provider.config.APIKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var apiResp gladiaTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Build result
	result := &types.TranscriptionResult{
		Text: apiResp.Result.Transcription.FullTranscript,
		Usage: types.TranscriptionUsage{
			DurationSeconds: apiResp.Metadata.Duration,
		},
	}

	// Add timestamps if requested
	if opts.Timestamps {
		result.Timestamps = make([]types.TranscriptionTimestamp, len(apiResp.Result.Transcription.Utterances))
		for i, utterance := range apiResp.Result.Transcription.Utterances {
			result.Timestamps[i] = types.TranscriptionTimestamp{
				Text:  utterance.Text,
				Start: utterance.Start,
				End:   utterance.End,
			}
		}
	}

	return result, nil
}
