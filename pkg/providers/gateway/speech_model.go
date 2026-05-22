package gateway

import (
	"context"
	"encoding/base64"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechModel implements the provider.SpeechModel interface for AI Gateway.
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new AI Gateway speech model.
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: provider, modelID: modelID}
}

func (m *SpeechModel) SpecificationVersion() string { return "v4" }
func (m *SpeechModel) Provider() string             { return "gateway" }
func (m *SpeechModel) ModelID() string              { return m.modelID }

// DoGenerate generates speech via the Gateway speech-model endpoint.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	body := map[string]interface{}{
		"text": opts.Text,
	}
	if opts.Voice != "" {
		body["voice"] = opts.Voice
	}
	if opts.OutputFormat != "" {
		body["outputFormat"] = opts.OutputFormat
	}
	if opts.Instructions != "" {
		body["instructions"] = opts.Instructions
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}
	if opts.Language != "" {
		body["language"] = opts.Language
	}
	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := m.getModelConfigHeaders()
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	var response gatewaySpeechResponse
	_, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/speech-model",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, (&LanguageModel{provider: m.provider, modelID: m.modelID}).handleErrorWithContext(ctx, err)
	}

	audio, err := base64.StdEncoding.DecodeString(response.Audio)
	if err != nil {
		return nil, err
	}
	return &types.SpeechResult{
		Audio:            audio,
		MimeType:         speechMimeType(opts.OutputFormat),
		Warnings:         response.Warnings,
		ProviderMetadata: response.ProviderMetadata,
		Usage:            types.SpeechUsage{CharacterCount: len(opts.Text)},
	}, nil
}

func (m *SpeechModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-speech-model-specification-version": "4",
		"ai-model-id":                           m.modelID,
	}
}

type gatewaySpeechResponse struct {
	Audio            string                 `json:"audio"`
	Warnings         []types.Warning        `json:"warnings,omitempty"`
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
}

func speechMimeType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/opus"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}
