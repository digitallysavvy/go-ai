package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements the provider.SpeechModel interface for Fal.ai.
//
// It ports @ai-sdk/fal's FalSpeechModel (fal-speech-model.ts). Requests
// always target https://fal.run/{modelId} regardless of the provider's
// configured BaseURL, matching the TS SDK's `url: ({ path }) => path`
// passthrough config (the configurable baseURL only applies to the image
// model).
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new Fal.ai speech synthesis model.
func NewSpeechModel(p *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "fal.speech" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

// falSpeechResponse mirrors falSpeechResponseSchema in fal-speech-model.ts.
type falSpeechResponse struct {
	Audio struct {
		URL string `json:"url"`
	} `json:"audio"`
	DurationMs *float64 `json:"duration_ms,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
}

// buildRequestBody mirrors FalSpeechModel.getArgs in fal-speech-model.ts.
func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}

	outputFormat := "url"
	if opts.OutputFormat == "hex" {
		outputFormat = "hex"
	}

	body := map[string]interface{}{
		"text":          opts.Text,
		"output_format": outputFormat,
	}
	if opts.Voice != "" {
		body["voice"] = opts.Voice
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}

	// Provider options (voice_setting, audio_setting, language_boost,
	// pronunciation_dict) are already snake_case on the wire, matching the
	// TS SDK's `...falOptions` spread into the top-level request body.
	if opts.ProviderOptions != nil {
		if falOpts, ok := opts.ProviderOptions["fal"].(map[string]interface{}); ok {
			for k, v := range falOpts {
				body[k] = v
			}
		}
	}

	// Language is not directly supported; warn and ignore.
	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: "fal speech models don't support 'language' directly; consider providerOptions.fal.language_boost",
		})
	}

	// Warn on invalid values (and on hex until we support hex response handling).
	if opts.OutputFormat != "" && opts.OutputFormat != "url" && opts.OutputFormat != "hex" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unsupported outputFormat: %s. Using 'url' instead.", opts.OutputFormat),
		})
	}

	return body, warnings
}

// DoGenerate performs speech synthesis, downloading the resulting audio URL.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	body, warnings := m.buildRequestBody(opts)
	currentDate := time.Now()

	requestPath := fmt.Sprintf("%s/%s", m.provider.speechHost, m.modelID)

	var jsonResp falSpeechResponse
	httpResp, err := m.provider.absClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    requestPath,
		Body:    body,
		Headers: opts.Headers,
	}, &jsonResp)
	if err != nil {
		return nil, m.handleError(err)
	}

	if jsonResp.Audio.URL == "" {
		return nil, providererrors.NewInvalidResponseDataError(jsonResp, "fal speech response missing audio.url")
	}

	// TS: getFromApi({ url: audioUrl, validateUrl: true, trustedOrigin: requestUrl }).
	// Hops same-origin with the submit request URL skip the generic SSRF
	// blocklist (which would otherwise reject the httptest loopback address
	// used in tests); every other origin is still fully validated.
	audioData, err := fileutil.Download(ctx, jsonResp.Audio.URL, fileutil.TrustedOriginDownloadOptions(requestPath, nil))
	if err != nil {
		return nil, fmt.Errorf("failed to download fal speech audio: %w", err)
	}

	requestBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fal speech request: %w", err)
	}

	var rawResponse interface{}
	_ = json.Unmarshal(httpResp.Body, &rawResponse)

	return &types.SpeechResult{
		Audio:    audioData,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      rawResponse,
		},
	}, nil
}

func (m *SpeechModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message := parseFalErrorMessage(statusErr.Body, string(statusErr.Body))
		providerErr := providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, "", message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		return providerErr
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}
