package elevenlabs

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechModel implements the provider.SpeechModel interface for ElevenLabs
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new ElevenLabs speech synthesis model
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "elevenlabs"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	voice := opts.Voice
	if voice == "" {
		voice = "21m00Tcm4TlvDq8ikWAM" // Default voice ID (Rachel)
	}

	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/v1/text-to-speech/%s", voice)
	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("elevenlabs", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ElevenLabs TTS API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
		MimeType: "audio/mpeg",
		Usage: types.SpeechUsage{
			CharacterCount: len(opts.Text),
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"text":     opts.Text,
		"model_id": m.modelID,
	}

	voiceSettings := map[string]interface{}{
		"stability":        0.5,
		"similarity_boost": 0.5,
	}

	if opts.Speed != nil {
		// ElevenLabs doesn't have a direct speed parameter,
		// but we can adjust stability
		voiceSettings["stability"] = *opts.Speed
	}

	body["voice_settings"] = voiceSettings

	return body
}
