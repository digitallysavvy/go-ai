package xai

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
)

type SpeechModel struct {
	provider *Provider
	modelID  string
}

func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: provider, modelID: modelID}
}

func (m *SpeechModel) SpecificationVersion() string { return "v4" }
func (m *SpeechModel) Provider() string             { return "xai.speech" }
func (m *SpeechModel) ModelID() string              { return m.modelID }

type XAISpeechProviderOptions struct {
	SampleRate               *int  `json:"sampleRate,omitempty"`
	BitRate                  *int  `json:"bitRate,omitempty"`
	OptimizeStreamingLatency *int  `json:"optimizeStreamingLatency,omitempty"`
	TextNormalization        *bool `json:"textNormalization,omitempty"`
}

func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	body, warnings, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now()

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/tts",
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, newXAIProviderError(m.Provider(), resp.StatusCode, resp.Body)
	}

	requestBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal xAI speech request metadata: %w", err)
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: timestamp,
			ModelID:   m.modelID,
			Headers:   flattenHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	xaiOpts, err := extractXAISpeechProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, nil, err
	}
	warnings := []types.Warning{}

	codec := "mp3"
	if opts.OutputFormat != "" {
		switch opts.OutputFormat {
		case "mp3", "wav", "pcm", "mulaw", "alaw":
			codec = opts.OutputFormat
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
			Details: "xAI speech models do not support the `instructions` option. Use xAI speech tags in `text` to control delivery.",
		})
	}

	outputFormat := map[string]interface{}{"codec": codec}
	if xaiOpts.SampleRate != nil {
		outputFormat["sample_rate"] = *xaiOpts.SampleRate
	}
	if xaiOpts.BitRate != nil {
		if codec == "mp3" {
			outputFormat["bit_rate"] = *xaiOpts.BitRate
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions",
				Details: "xAI `bitRate` is supported only for mp3 output. It was ignored.",
			})
		}
	}

	voice := opts.Voice
	if voice == "" {
		voice = "eve"
	}
	language := opts.Language
	if language == "" {
		language = "auto"
	}

	body := map[string]interface{}{
		"text":          opts.Text,
		"voice_id":      voice,
		"language":      language,
		"output_format": outputFormat,
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}
	if xaiOpts.OptimizeStreamingLatency != nil {
		body["optimize_streaming_latency"] = *xaiOpts.OptimizeStreamingLatency
	}
	if xaiOpts.TextNormalization != nil {
		body["text_normalization"] = *xaiOpts.TextNormalization
	}
	return body, warnings, nil
}

func extractXAISpeechProviderOptions(providerOptions map[string]interface{}) (XAISpeechProviderOptions, error) {
	var opts XAISpeechProviderOptions
	raw, ok := providerOptions["xai"]
	if !ok || raw == nil {
		return opts, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if opts.SampleRate != nil && !xaiIntIn(*opts.SampleRate, 8000, 16000, 22050, 24000, 44100, 48000) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("sampleRate must be one of 8000, 16000, 22050, 24000, 44100, or 48000"))
	}
	if opts.BitRate != nil && !xaiIntIn(*opts.BitRate, 32000, 64000, 96000, 128000, 192000) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("bitRate must be one of 32000, 64000, 96000, 128000, or 192000"))
	}
	if opts.OptimizeStreamingLatency != nil && !xaiIntIn(*opts.OptimizeStreamingLatency, 0, 1, 2) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("optimizeStreamingLatency must be 0, 1, or 2"))
	}
	return opts, nil
}
