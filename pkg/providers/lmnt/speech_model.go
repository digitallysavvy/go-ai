package lmnt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechModel represents an LMNT speech synthesis model
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v1"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "lmnt"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// lmntSpeechRequest represents the LMNT API request
type lmntSpeechRequest struct {
	Text   string  `json:"text"`
	Voice  string  `json:"voice"`
	Speed  float64 `json:"speed,omitempty"`
	Format string  `json:"format,omitempty"`
}

// lmntSpeechResponse represents the LMNT API response
type lmntSpeechResponse struct {
	Audio    []byte `json:"audio"`
	Duration float64 `json:"duration"`
}

// DoGenerate synthesizes speech from text
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	// Build request
	reqBody := lmntSpeechRequest{
		Text:   opts.Text,
		Voice:  opts.Voice,
		Format: "mp3",
	}

	if opts.Speed != nil {
		reqBody.Speed = *opts.Speed
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/synthesize", m.provider.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", m.provider.config.APIKey)

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

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Build result
	result := &types.SpeechResult{
		Audio:    audioData,
		MimeType: "audio/mpeg",
		Usage: types.SpeechUsage{
			CharacterCount: len(opts.Text),
		},
	}

	return result, nil
}
