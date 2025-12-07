package assemblyai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for AssemblyAI
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new AssemblyAI transcription model
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
	return "assemblyai"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	// Step 1: Upload audio
	uploadResp, err := m.provider.client.Post(ctx, "/upload", map[string]interface{}{
		"audio": opts.Audio,
	})
	if err != nil {
		return nil, providererrors.NewProviderError("assemblyai", 0, "", err.Error(), err)
	}

	var uploadResult struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(uploadResp.Body, &uploadResult); err != nil {
		return nil, fmt.Errorf("failed to decode upload response: %w", err)
	}

	// Step 2: Create transcription
	reqBody := map[string]interface{}{
		"audio_url": uploadResult.UploadURL,
	}

	if opts.Language != "" {
		reqBody["language_code"] = opts.Language
	}

	if m.modelID != "best" {
		reqBody["speech_model"] = m.modelID
	}

	transcriptResp, err := m.provider.client.Post(ctx, "/transcript", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("assemblyai", 0, "", err.Error(), err)
	}

	var transcript assemblyAITranscript
	if err := json.Unmarshal(transcriptResp.Body, &transcript); err != nil {
		return nil, fmt.Errorf("failed to decode transcript response: %w", err)
	}

	// Step 3: Poll for completion
	transcript, err = m.pollTranscript(ctx, transcript.ID)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(transcript), nil
}

func (m *TranscriptionModel) pollTranscript(ctx context.Context, transcriptID string) (assemblyAITranscript, error) {
	maxAttempts := 60
	pollInterval := 2 * time.Second

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return assemblyAITranscript{}, ctx.Err()
		default:
		}

		resp, err := m.provider.client.Get(ctx, "/transcript/"+transcriptID)
		if err != nil {
			return assemblyAITranscript{}, err
		}

		var transcript assemblyAITranscript
		if err := json.Unmarshal(resp.Body, &transcript); err != nil {
			return assemblyAITranscript{}, fmt.Errorf("failed to decode transcript: %w", err)
		}

		if transcript.Status == "completed" {
			return transcript, nil
		}

		if transcript.Status == "error" {
			return assemblyAITranscript{}, fmt.Errorf("transcription failed: %s", transcript.Error)
		}

		time.Sleep(pollInterval)
	}

	return assemblyAITranscript{}, fmt.Errorf("transcription timed out after %d attempts", maxAttempts)
}

func (m *TranscriptionModel) convertResponse(transcript assemblyAITranscript) *types.TranscriptionResult {
	var timestamps []types.TranscriptionTimestamp
	for _, word := range transcript.Words {
		timestamps = append(timestamps, types.TranscriptionTimestamp{
			Text:  word.Text,
			Start: float64(word.Start) / 1000.0, // Convert ms to seconds
			End:   float64(word.End) / 1000.0,
		})
	}

	return &types.TranscriptionResult{
		Text:       transcript.Text,
		Timestamps: timestamps,
		Usage: types.TranscriptionUsage{
			DurationSeconds: float64(transcript.AudioDuration) / 1000.0,
		},
	}
}

type assemblyAITranscript struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Text          string `json:"text"`
	AudioDuration int    `json:"audio_duration"`
	Error         string `json:"error"`
	Words         []struct {
		Text  string `json:"text"`
		Start int    `json:"start"`
		End   int    `json:"end"`
	} `json:"words"`
}
