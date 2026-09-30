package xai

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

// XAISpeechProviderOptions holds xAI-specific text-to-speech options, sent
// under providerOptions.xai. Mirrors TS xaiSpeechModelOptionsSchema.
type XAISpeechProviderOptions struct {
	SampleRate               *int              `json:"sampleRate,omitempty"`
	BitRate                  *int              `json:"bitRate,omitempty"`
	OptimizeStreamingLatency *int              `json:"optimizeStreamingLatency,omitempty"`
	TextNormalization        *bool             `json:"textNormalization,omitempty"`
	WithTimestamps           *bool             `json:"withTimestamps,omitempty"`
	Replace                  map[string]string `json:"replace,omitempty"`
}

// xaiSpeechAudioTimestamps holds character-level timing metadata returned in
// the `with_timestamps` JSON envelope.
type xaiSpeechAudioTimestamps struct {
	GraphChars []string     `json:"graph_chars"`
	GraphTimes [][2]float64 `json:"graph_times"`
}

// xaiSpeechTimestampsResponse is the JSON envelope xAI returns instead of raw
// audio bytes when `with_timestamps` is requested: base64-encoded audio plus
// character-level timing metadata.
type xaiSpeechTimestampsResponse struct {
	Audio           *string                   `json:"audio,omitempty"`
	ContentType     *string                   `json:"content_type,omitempty"`
	Duration        *float64                  `json:"duration,omitempty"`
	AudioTimestamps *xaiSpeechAudioTimestamps `json:"audio_timestamps,omitempty"`
}

func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	xaiOpts, err := extractXAISpeechProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
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
	// xAI returns a trace id on every response, success and error.
	traceID := resp.Headers.Get("x-trace-id")
	if resp.StatusCode >= 400 {
		message := parseXAIErrorMessage(resp.Body)
		if traceID != "" {
			message = fmt.Sprintf("%s (trace: %s)", message, traceID)
		}
		return nil, providererrors.NewProviderError(m.Provider(), resp.StatusCode, "", message, nil)
	}

	requestBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal xAI speech request metadata: %w", err)
	}

	// With `with_timestamps` the API returns a JSON envelope carrying
	// base64-encoded audio plus character-level timings instead of raw audio
	// bytes.
	withTimestamps := xaiOpts.WithTimestamps != nil && *xaiOpts.WithTimestamps
	audio := resp.Body
	var envelope *xaiSpeechTimestampsResponse
	if withTimestamps {
		var parsed xaiSpeechTimestampsResponse
		if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			return nil, providererrors.NewProviderError(m.Provider(), 0, "",
				fmt.Sprintf("failed to decode speech timestamps envelope: %v", err), err)
		}
		envelope = &parsed
		if envelope.Audio != nil && *envelope.Audio != "" {
			decoded, decErr := base64.StdEncoding.DecodeString(*envelope.Audio)
			if decErr != nil {
				return nil, providererrors.NewProviderError(m.Provider(), 0, "",
					fmt.Sprintf("failed to decode base64 audio: %v", decErr), decErr)
			}
			audio = decoded
		} else {
			// Empty audio is returned as-is so the core layer can surface
			// NoSpeechGeneratedError the same way the TS SDK does.
			audio = []byte{}
		}
	}

	xaiMetadata := map[string]interface{}{}
	if traceID != "" {
		xaiMetadata["traceId"] = traceID
	}
	if envelope != nil {
		if envelope.Duration != nil {
			xaiMetadata["duration"] = *envelope.Duration
		}
		if envelope.ContentType != nil {
			xaiMetadata["contentType"] = *envelope.ContentType
		}
		if envelope.AudioTimestamps != nil {
			xaiMetadata["audioTimestamps"] = map[string]interface{}{
				"graphChars": envelope.AudioTimestamps.GraphChars,
				"graphTimes": envelope.AudioTimestamps.GraphTimes,
			}
		}
	}
	var providerMetadata map[string]interface{}
	if len(xaiMetadata) > 0 {
		providerMetadata = map[string]interface{}{"xai": xaiMetadata}
	}

	return &types.SpeechResult{
		Audio:            audio,
		Warnings:         warnings,
		ProviderMetadata: providerMetadata,
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
	if xaiOpts.WithTimestamps != nil {
		body["with_timestamps"] = *xaiOpts.WithTimestamps
	}
	if len(xaiOpts.Replace) > 0 {
		body["replace"] = xaiOpts.Replace
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
