package openai

import (
	"context"
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

// SpeechModel implements the provider.SpeechModel interface for OpenAI TTS
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new OpenAI speech synthesis model
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return m.provider.Name()
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody, warnings := m.buildRequestBody(opts)
	currentDate := time.Now()

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/audio/speech",
		Body:    reqBody,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LOpenAI TTS API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}
	requestBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OpenAI speech request metadata: %w", err)
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
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

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	body := map[string]interface{}{
		"model":           m.modelID,
		"input":           opts.Text,
		"response_format": "mp3",
	}
	if opts.Voice != "" {
		body["voice"] = opts.Voice
	} else {
		body["voice"] = "alloy"
	}
	warnings := make([]types.Warning, 0)
	if opts.OutputFormat != "" {
		switch opts.OutputFormat {
		case "mp3", "opus", "aac", "flac", "wav", "pcm":
			body["response_format"] = opts.OutputFormat
		default:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported output format: %s. Using mp3 instead.", opts.OutputFormat),
			})
		}
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}
	if opts.Instructions != "" {
		body["instructions"] = opts.Instructions
	}
	// providerOptions.openai.speed/instructions take priority over the
	// top-level fields when explicitly set (e13c32f; no speed default).
	if opts.ProviderOptions != nil {
		if openaiOpts, ok := opts.ProviderOptions["openai"].(map[string]interface{}); ok {
			if v, ok := speechSpeedOption(openaiOpts["speed"]); ok {
				body["speed"] = v
			}
			if instructions, ok := openaiOpts["instructions"].(string); ok && instructions != "" {
				body["instructions"] = instructions
			}
		}
	}
	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: fmt.Sprintf("OpenAI speech models do not support language selection. Language parameter %q was ignored.", opts.Language),
		})
	}
	return body, warnings
}

// speechSpeedOption converts a JSON-decoded providerOptions.openai.speed
// value (typically float64, but tolerate int/json.Number) to float64.
func speechSpeedOption(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
