package deepgram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for Deepgram
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Deepgram transcription model
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
	return "deepgram"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	// Build query parameters
	query := map[string]string{
		"model": m.modelID,
	}

	if opts.Language != "" {
		query["language"] = opts.Language
	}

	if opts.Timestamps {
		query["punctuate"] = "true"
		query["utterances"] = "true"
	}

	// Send audio data
	req := internalhttp.Request{
		Method: "POST",
		Path:   "/v1/listen",
		Body:   bytes.NewReader(opts.Audio),
		Headers: map[string]string{
			"Content-Type": opts.MimeType,
		},
		Query: query,
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		return nil, providererrors.NewProviderError("deepgram", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Deepgram API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var response deepgramTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode transcription response: %w", err)
	}

	return m.convertResponse(response), nil
}

func (m *TranscriptionModel) convertResponse(response deepgramTranscriptionResponse) *types.TranscriptionResult {
	if len(response.Results.Channels) == 0 || len(response.Results.Channels[0].Alternatives) == 0 {
		return &types.TranscriptionResult{
			Text: "",
			Usage: types.TranscriptionUsage{
				DurationSeconds: response.Metadata.Duration,
			},
		}
	}

	transcript := response.Results.Channels[0].Alternatives[0].Transcript

	// Extract timestamps if available
	var timestamps []types.TranscriptionTimestamp
	for _, word := range response.Results.Channels[0].Alternatives[0].Words {
		timestamps = append(timestamps, types.TranscriptionTimestamp{
			Text:  word.Word,
			Start: word.Start,
			End:   word.End,
		})
	}

	return &types.TranscriptionResult{
		Text:       transcript,
		Timestamps: timestamps,
		Usage: types.TranscriptionUsage{
			DurationSeconds: response.Metadata.Duration,
		},
	}
}

type deepgramTranscriptionResponse struct {
	Metadata struct {
		TransactionKey string  `json:"transaction_key"`
		RequestID      string  `json:"request_id"`
		Duration       float64 `json:"duration"`
	} `json:"metadata"`
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []struct {
					Word       string  `json:"word"`
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
				} `json:"words"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}
