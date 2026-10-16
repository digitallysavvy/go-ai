package openai

import (
	"context"
	"encoding/json"
	"errors"
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
	return m.provider.Name() + ".speech"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody, warnings, err := m.buildRequestBodyChecked(opts)
	if err != nil {
		return nil, err
	}
	currentDate := time.Now()

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/audio/speech",
		Query:   m.provider.config.AudioQuery,
		Body:    reqBody,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, internalhttp.StatusProviderError("openai", resp.StatusCode, resp.Headers, resp.Body)
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
		},
	}, nil
}

// buildRequestBodyChecked is buildRequestBody plus TS parseProviderOptions
// validation of providerOptions.openai (instructions: string|null,
// speed: number in [0.25, 4]|null).
func (m *SpeechModel) buildRequestBodyChecked(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	if err := validateSpeechProviderOptions(opts.ProviderOptions); err != nil {
		return nil, nil, err
	}
	body, warnings := m.buildRequestBody(opts)
	return body, warnings, nil
}

func validateSpeechProviderOptions(providerOptions map[string]interface{}) error {
	raw, ok := providerOptions["openai"]
	if !ok || raw == nil {
		return nil
	}
	invalid := func(msg string) error {
		return &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "invalid openai provider options",
			Cause:   errors.New(msg),
		}
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return invalid("Invalid input: expected object")
	}
	if v, ok := m["instructions"]; ok && v != nil {
		if _, isString := v.(string); !isString {
			return invalid("Invalid input: expected string at instructions")
		}
	}
	if v, ok := m["speed"]; ok && v != nil {
		f, isNum := speechSpeedOption(v)
		if !isNum {
			return invalid("Invalid input: expected number at speed")
		}
		if f < 0.25 {
			return invalid("Too small: expected number to be >=0.25 at speed")
		}
		if f > 4 {
			return invalid("Too big: expected number to be <=4 at speed")
		}
	}
	return nil
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
			if instructions, ok := openaiOpts["instructions"].(string); ok {
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
