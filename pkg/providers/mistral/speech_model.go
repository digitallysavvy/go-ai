package mistral

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements the provider.SpeechModel interface for Mistral's
// Voxtral text-to-speech models (POST /v1/audio/speech). Generation is
// non-streaming; both saved voice IDs (Voice) and one-off reference audio
// clips (providerOptions.mistral.refAudio, base64-encoded) are supported,
// mirroring ai/packages/mistral/src/mistral-speech-model.ts.
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new Mistral speech synthesis model.
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "mistral" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

// DoGenerate performs non-streaming speech synthesis.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	currentDate := time.Now()
	reqBody, warnings := m.buildRequestBody(opts)

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/audio/speech",
		Body:    reqBody,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
	}

	var response mistralSpeechResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("mistral: failed to decode speech response: %w", err)
	}
	audio, err := base64.StdEncoding.DecodeString(response.AudioData)
	if err != nil {
		return nil, fmt.Errorf("mistral: failed to decode audio_data: %w", err)
	}

	requestBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("mistral: failed to marshal speech request metadata: %w", err)
	}

	return &types.SpeechResult{
		Audio:    audio,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}, nil
}

// buildRequestBody builds the /v1/audio/speech request body. Mistral speech
// models do not support instructions/speed/language, so those options
// produce "unsupported" warnings rather than being forwarded (matching TS).
// refAudio, when present via providerOptions.mistral.refAudio, takes
// precedence over Voice for the voice_id field, matching TS exactly.
func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	var warnings []types.Warning

	responseFormat := "mp3"
	if opts.OutputFormat != "" {
		switch opts.OutputFormat {
		case "pcm", "wav", "mp3", "flac", "opus":
			responseFormat = opts.OutputFormat
		default:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported output format: %s. Using mp3 instead.", opts.OutputFormat),
			})
		}
	}

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Mistral speech models do not support the `instructions` option. Use a reference audio clip to guide delivery.",
		})
	}
	if opts.Speed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "speed",
			Details: "Mistral speech models do not support the `speed` option. It was ignored.",
		})
	}
	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: "Mistral speech models do not support the `language` option. Language is inferred from the input text and voice.",
		})
	}

	refAudio := ""
	if opts.ProviderOptions != nil {
		if mistralOpts, ok := opts.ProviderOptions["mistral"].(map[string]interface{}); ok {
			if v, ok := mistralOpts["refAudio"].(string); ok {
				refAudio = v
			}
		}
	}

	body := map[string]interface{}{
		"model":           m.modelID,
		"input":           opts.Text,
		"response_format": responseFormat,
		"stream":          false,
	}
	if refAudio != "" {
		body["ref_audio"] = refAudio
	} else if opts.Voice != "" {
		body["voice_id"] = opts.Voice
	}

	return body, warnings
}

type mistralSpeechResponse struct {
	AudioData string `json:"audio_data"`
}
