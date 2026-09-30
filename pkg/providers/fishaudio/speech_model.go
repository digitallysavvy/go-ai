package fishaudio

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements provider.SpeechModel for Fish Audio (POST /v1/tts).
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// supportedSpeechFormats mirrors the TypeScript SDK's SUPPORTED_FORMATS.
var supportedSpeechFormats = map[string]bool{"wav": true, "pcm": true, "mp3": true, "opus": true}

const defaultSpeechFormat = "mp3"

// Fish Audio accepts prosody.speed between 0.5 and 2.0.
const (
	minSpeechSpeed = 0.5
	maxSpeechSpeed = 2.0
)

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "fish-audio.speech" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

// DoGenerate performs text-to-speech synthesis.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()
	requestBody, warnings := m.buildRequestBody(opts)

	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/tts",
		Body:   requestBytes,
		// Fish Audio selects the TTS model with a `model` HTTP header rather
		// than a request body field.
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"model": m.modelID}),
	})
	if err != nil {
		return nil, handleError(err)
	}
	if resp.StatusCode >= 400 {
		return nil, handleError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
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
	warnings := []types.Warning{}
	fishOpts := extractSpeechModelOptions(opts.ProviderOptions)

	format := resolveSpeechFormat(opts.OutputFormat, &warnings)

	body := map[string]interface{}{
		"text":   opts.Text,
		"format": format,
	}

	// providerOptions.fishAudio.referenceId wins over the generic voice so
	// that multi-speaker arrays are expressible.
	var referenceID interface{}
	if fishOpts != nil && fishOpts.ReferenceID != nil {
		referenceID = fishOpts.ReferenceID
	} else if opts.Voice != "" {
		referenceID = opts.Voice
	}
	if referenceID != nil {
		body["reference_id"] = referenceID
	}

	prosody := map[string]interface{}{}
	if opts.Speed != nil {
		speed := *opts.Speed
		if speed >= minSpeechSpeed && speed <= maxSpeechSpeed {
			prosody["speed"] = speed
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "speed",
				Details: "Fish Audio speed must be between 0.5 and 2. The speed option was ignored.",
			})
		}
	}

	if fishOpts != nil {
		if fishOpts.Volume != nil {
			prosody["volume"] = *fishOpts.Volume
		}
		if fishOpts.NormalizeLoudness != nil {
			// Fish Audio accepts normalize_loudness on s1 but ignores it, so
			// warn rather than let it silently do nothing.
			if m.modelID == ModelS1 {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions.fishAudio.normalizeLoudness",
					Details: "Fish Audio ignores normalizeLoudness on s1. It is supported by the S2 family (s2-pro, s2.1-pro).",
				})
			} else {
				prosody["normalize_loudness"] = *fishOpts.NormalizeLoudness
			}
		}
	}
	if len(prosody) > 0 {
		body["prosody"] = prosody
	}

	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: "Fish Audio infers the language from the input text and the selected voice, and has no language parameter. The language option was ignored.",
		})
	}
	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Fish Audio does not support instructions. The instructions option was ignored.",
		})
	}

	if fishOpts != nil {
		if fishOpts.SampleRate != nil {
			body["sample_rate"] = *fishOpts.SampleRate
		}
		if fishOpts.Mp3Bitrate != nil {
			if format == "mp3" {
				body["mp3_bitrate"] = *fishOpts.Mp3Bitrate
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions.fishAudio.mp3Bitrate",
					Details: "mp3Bitrate only applies to mp3 output. The option was ignored for " + format + " output.",
				})
			}
		}
		if fishOpts.OpusBitrate != nil {
			if format == "opus" {
				body["opus_bitrate"] = *fishOpts.OpusBitrate
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions.fishAudio.opusBitrate",
					Details: "opusBitrate only applies to opus output. The option was ignored for " + format + " output.",
				})
			}
		}
		if fishOpts.Latency != "" {
			body["latency"] = fishOpts.Latency
		}
		if fishOpts.Temperature != nil {
			body["temperature"] = *fishOpts.Temperature
		}
		if fishOpts.TopP != nil {
			body["top_p"] = *fishOpts.TopP
		}
		if fishOpts.ChunkLength != nil {
			body["chunk_length"] = *fishOpts.ChunkLength
		}
		if fishOpts.MinChunkLength != nil {
			body["min_chunk_length"] = *fishOpts.MinChunkLength
		}
		if fishOpts.Normalize != nil {
			body["normalize"] = *fishOpts.Normalize
		}
		if fishOpts.MaxNewTokens != nil {
			body["max_new_tokens"] = *fishOpts.MaxNewTokens
		}
		if fishOpts.RepetitionPenalty != nil {
			body["repetition_penalty"] = *fishOpts.RepetitionPenalty
		}
		if fishOpts.ConditionOnPreviousChunks != nil {
			body["condition_on_previous_chunks"] = *fishOpts.ConditionOnPreviousChunks
		}
		if fishOpts.EarlyStopThreshold != nil {
			body["early_stop_threshold"] = *fishOpts.EarlyStopThreshold
		}
		if len(fishOpts.Features) > 0 {
			body["features"] = fishOpts.Features
		}
	}

	return body, warnings
}

// resolveSpeechFormat mirrors the TypeScript SDK's resolveFormat: normalizes
// case, falls back to mp3 with a warning for unsupported formats.
func resolveSpeechFormat(outputFormat string, warnings *[]types.Warning) string {
	if outputFormat == "" {
		return defaultSpeechFormat
	}
	normalized := strings.ToLower(outputFormat)
	if supportedSpeechFormats[normalized] {
		return normalized
	}
	*warnings = append(*warnings, types.Warning{
		Type:    "unsupported",
		Feature: "outputFormat",
		Details: "Fish Audio does not support the output format \"" + outputFormat + "\". Falling back to mp3. Supported formats are wav, pcm, mp3, opus.",
	})
	return defaultSpeechFormat
}
