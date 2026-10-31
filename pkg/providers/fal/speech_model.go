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
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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

// buildOrderedRequestBody mirrors FalSpeechModel.getArgs in fal-speech-model.ts
// with the JS object key order (text, output_format, voice, speed, then
// provider options), which JSON.stringify(requestBody) records in request.body.
func (m *SpeechModel) buildOrderedRequestBody(opts *provider.SpeechGenerateOptions) (*jsonorder.Object, []types.Warning) {
	warnings := []types.Warning{}

	outputFormat := "url"
	if opts.OutputFormat == "hex" {
		outputFormat = "hex"
	}

	body := jsonorder.New()
	body.Set("text", opts.Text)
	body.Set("output_format", outputFormat)
	if opts.Voice != "" {
		body.Set("voice", opts.Voice)
	}
	if opts.Speed != nil {
		body.Set("speed", *opts.Speed)
	}

	// Provider options (voice_setting, audio_setting, language_boost,
	// pronunciation_dict) are already snake_case on the wire, matching the
	// TS SDK's `...falOptions` spread into the top-level request body.
	if opts.ProviderOptions != nil {
		if falOpts, ok := opts.ProviderOptions["fal"].(map[string]interface{}); ok {
			// zod looseObject output: the schema's keys in schema order, then
			// the unknown keys.
			known := []string{"voice_setting", "audio_setting", "language_boost", "pronunciation_dict"}
			rest := map[string]interface{}{}
			for k, v := range falOpts {
				rest[k] = v
			}
			for _, k := range known {
				v, ok := falOpts[k]
				if !ok {
					continue
				}
				delete(rest, k)
				if vs, isMap := v.(map[string]interface{}); isMap && k == "voice_setting" {
					// z.object strips unknown keys and follows the schema order.
					ordered := jsonorder.New()
					for _, sk := range []string{"speed", "vol", "voice_id", "pitch", "english_normalization", "emotion"} {
						if sv, ok := vs[sk]; ok {
							ordered.Set(sk, sv)
						}
					}
					v = ordered
				}
				body.Set(k, v)
			}
			for _, k := range jsonorder.FromMap(rest).Keys() {
				body.Set(k, rest[k])
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
	if err := validateSpeechOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	body, warnings := m.buildOrderedRequestBody(opts)
	currentDate := time.Now()

	requestPath := fmt.Sprintf("%s/%s", m.provider.speechHost, m.modelID)

	var jsonResp falSpeechResponse
	httpResp, err := m.provider.absClient.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaFalSpeech),
		Method:           http.MethodPost,
		Path:             requestPath,
		Body:             body,
		Headers:          opts.Headers,
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
		// TS getFromApi with createStatusCodeErrorResponseHandler.
		return nil, providerutils.StatusCodeDownloadError("fal", err)
	}

	var rawResponse interface{}
	_ = json.Unmarshal(httpResp.Body, &rawResponse)

	return &types.SpeechResult{
		Audio:    audioData,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: jsonorder.Stringify(body),
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
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message := parseFalErrorMessage(statusErr.Body, http.StatusText(statusErr.StatusCode))
		return providererrors.NewProviderErrorFromResponse(m.Provider(), 0, "", message, err)
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}
