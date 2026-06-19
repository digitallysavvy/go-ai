package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements the provider.SpeechModel interface for Azure OpenAI
type SpeechModel struct {
	provider     *Provider
	deploymentID string
}

// NewSpeechModel creates a new Azure OpenAI speech synthesis model
func NewSpeechModel(provider *Provider, deploymentID string) *SpeechModel {
	return &SpeechModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "azure.speech"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *SpeechModel) ModelID() string {
	return m.deploymentID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody, warnings := m.buildRequestArgs(opts)
	currentDate := time.Now()

	// Azure OpenAI speech generation endpoint
	path := m.provider.endpointPath(m.deploymentID, "/audio/speech")
	headers, err := m.provider.requestHeaders(ctx, opts.Headers)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    path,
		Body:    reqBody,
		Headers: headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("azure OpenAI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}
	requestBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Azure speech request metadata: %w", err)
	}

	// The response is the raw audio bytes
	return &types.SpeechResult{
		Audio:    resp.Body,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.deploymentID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) map[string]interface{} {
	body, _ := m.buildRequestArgs(opts)
	return body
}

func (m *SpeechModel) buildRequestArgs(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	reqBody := map[string]interface{}{
		"input":           opts.Text,
		"model":           m.deploymentID,
		"response_format": "mp3",
	}

	if opts.Voice != "" {
		reqBody["voice"] = opts.Voice
	} else {
		reqBody["voice"] = "alloy" // Default voice
	}

	warnings := make([]types.Warning, 0)
	if opts.OutputFormat != "" {
		switch opts.OutputFormat {
		case "mp3", "opus", "aac", "flac", "wav", "pcm":
			reqBody["response_format"] = opts.OutputFormat
		default:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported output format: %s. Using mp3 instead.", opts.OutputFormat),
			})
		}
	}
	if opts.Speed != nil {
		reqBody["speed"] = *opts.Speed
	}
	if opts.Instructions != "" {
		reqBody["instructions"] = opts.Instructions
	}
	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: fmt.Sprintf("OpenAI speech models do not support language selection. Language parameter %q was ignored.", opts.Language),
		})
	}

	return reqBody, warnings
}
