package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// voiceFamilyIDs are bare model family IDs that compose the upstream model ID
// from the voice and language call options (`<family>-<voice>-<language>`,
// e.g. `aura-2-thalia-en`). Full voice IDs pass through unchanged.
var voiceFamilyIDs = map[string]bool{"aura": true, "aura-2": true}

// SpeechModel implements the provider.SpeechModel interface for Deepgram
// text-to-speech (POST /v1/speak).
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new Deepgram speech synthesis model.
func NewSpeechModel(p *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "deepgram" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

func extractSpeechModelOptions(providerOptions map[string]interface{}) *SpeechModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["deepgram"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts SpeechModelOptions
	if err := json.Unmarshal(b, &opts); err != nil {
		return nil
	}
	return &opts
}

// DoGenerate performs speech synthesis via Deepgram's REST TTS endpoint.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()

	queryParams, warnings, err := m.buildArgs(opts)
	if err != nil {
		return nil, err
	}

	requestBody := map[string]interface{}{"text": opts.Text}
	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/speak",
		Body:    requestBody,
		Query:   queryParams,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, handleError("deepgram", err)
	}
	if resp.StatusCode >= 400 {
		return nil, handleError("deepgram", &internalhttp.HTTPStatusError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		})
	}

	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("deepgram: failed to marshal speech request metadata: %w", err)
	}

	result := &types.SpeechResult{
		Audio:            resp.Body,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"deepgram": buildSpeechProviderMetadata(resp.Headers)},
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}
	// TS: `...(charCount != null ? { usage: { characters: charCount } } : {})`.
	if charCount := headerInt(resp.Headers, "Dg-Char-Count"); charCount != nil {
		result.Usage = map[string]interface{}{"characters": *charCount}
	}
	return result, nil
}

func (m *SpeechModel) buildArgs(opts *provider.SpeechGenerateOptions) (map[string]string, []types.Warning, error) {
	warnings := []types.Warning{}
	dgOpts := extractSpeechModelOptions(opts.ProviderOptions)

	// Compose the upstream model ID from voice/language when a bare voice
	// family ID is used; full voice IDs pass through.
	upstreamModelID := m.modelID
	if voiceFamilyIDs[m.modelID] {
		trimmedVoice := strings.TrimSpace(opts.Voice)
		if trimmedVoice == "" {
			return nil, nil, fmt.Errorf("deepgram: speech model %q requires a `voice` to be set (e.g. voice: 'thalia')", m.modelID)
		}
		language := opts.Language
		if language == "auto" {
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "language",
				Details: `Deepgram TTS models do not support automatic language detection. Language "en" was used instead.`,
			})
		}
		lang := "en"
		if language != "" && language != "auto" {
			lang = language
		}
		upstreamModelID = fmt.Sprintf("%s-%s-%s", m.modelID, trimmedVoice, lang)
	}

	query := map[string]string{"model": upstreamModelID}

	if opts.OutputFormat != "" {
		applyOutputFormat(query, opts.OutputFormat)
	}

	if dgOpts != nil {
		applyDeepgramSpeechOptions(query, dgOpts, &warnings)
	}

	// Voice parameter is only meaningful for bare voice-family IDs, where it
	// composes the upstream model. A full voice model ID cannot honor it.
	if upstreamModelID == m.modelID && opts.Voice != "" && opts.Voice != m.modelID {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "voice",
			Details: fmt.Sprintf(`Deepgram TTS models embed the voice in the model ID. The voice parameter "%s" was ignored. Use the model ID to select a voice (e.g., "aura-2-helena-en").`, opts.Voice),
		})
	}

	if opts.Speed != nil {
		query["speed"] = strconv.FormatFloat(*opts.Speed, 'f', -1, 64)
	}

	if upstreamModelID == m.modelID && opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: fmt.Sprintf(`Deepgram TTS models are language-specific via the model ID. Language parameter "%s" was ignored. Select a model with the appropriate language suffix (e.g., "-en" for English).`, opts.Language),
		})
	}

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: `Deepgram TTS REST API does not support instructions. Instructions parameter was ignored.`,
		})
	}

	return query, warnings, nil
}

// applyOutputFormat maps the shared outputFormat option to Deepgram's
// encoding/container/sample_rate/bit_rate query parameters.
// https://developers.deepgram.com/docs/tts-media-output-settings#audio-format-combinations
func applyOutputFormat(query map[string]string, outputFormat string) {
	formatLower := strings.ToLower(outputFormat)

	type mapped struct {
		encoding   string
		container  string
		sampleRate int
	}
	formatMap := map[string]mapped{
		"mp3":      {encoding: "mp3"},
		"wav":      {container: "wav", encoding: "linear16"},
		"linear16": {encoding: "linear16", container: "wav"},
		"mulaw":    {encoding: "mulaw", container: "wav"},
		"alaw":     {encoding: "alaw", container: "wav"},
		"opus":     {encoding: "opus", container: "ogg"},
		"ogg":      {encoding: "opus", container: "ogg"},
		"flac":     {encoding: "flac"},
		"aac":      {encoding: "aac"},
		"pcm":      {encoding: "linear16", container: "none"},
	}

	if mf, ok := formatMap[formatLower]; ok {
		if mf.encoding != "" {
			query["encoding"] = mf.encoding
		}
		if mf.container != "" {
			query["container"] = mf.container
		}
		if mf.sampleRate != 0 {
			query["sample_rate"] = strconv.Itoa(mf.sampleRate)
		}
		return
	}

	// Try to parse a format like "wav_44100" or "linear16_24000".
	parts := strings.SplitN(formatLower, "_", 2)
	if len(parts) < 2 {
		return
	}
	firstPart, secondPart := parts[0], parts[1]
	sampleRate, sampleRateErr := strconv.Atoi(secondPart)

	switch firstPart {
	case "linear16", "mulaw", "alaw", "mp3", "opus", "flac", "aac":
		query["encoding"] = firstPart
		switch firstPart {
		case "linear16", "mulaw", "alaw":
			query["container"] = "wav"
		case "opus":
			query["container"] = "ogg"
		}
		if sampleRateErr == nil {
			validRates := map[string][]int{
				"linear16": {8000, 16000, 24000, 32000, 48000},
				"mulaw":    {8000, 16000},
				"alaw":     {8000, 16000},
				"flac":     {8000, 16000, 22050, 32000, 48000},
			}
			if rates, ok := validRates[firstPart]; ok && intInSlice(rates, sampleRate) {
				query["sample_rate"] = strconv.Itoa(sampleRate)
			}
		}
	case "wav", "ogg":
		if firstPart == "wav" {
			query["container"] = "wav"
			query["encoding"] = "linear16"
		} else {
			query["container"] = "ogg"
			query["encoding"] = "opus"
		}
		if sampleRateErr == nil {
			query["sample_rate"] = strconv.Itoa(sampleRate)
		}
	}
}

func intInSlice(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// applyDeepgramSpeechOptions applies provider-specific TTS options, mapping
// camelCase fields to Deepgram's snake_case query params and validating
// combinations per Deepgram's spec.
func applyDeepgramSpeechOptions(query map[string]string, opts *SpeechModelOptions, warnings *[]types.Warning) {
	if opts.Encoding != "" {
		newEncoding := strings.ToLower(opts.Encoding)
		query["encoding"] = newEncoding

		if opts.Container != "" {
			container := strings.ToLower(opts.Container)
			switch newEncoding {
			case "linear16", "mulaw", "alaw":
				if container != "wav" && container != "none" {
					*warnings = append(*warnings, types.Warning{
						Type:    "unsupported",
						Feature: "providerOptions",
						Details: fmt.Sprintf(`Encoding "%s" only supports containers "wav" or "none". Container "%s" was ignored.`, newEncoding, opts.Container),
					})
				} else {
					query["container"] = container
				}
			case "opus":
				query["container"] = "ogg"
			case "mp3", "flac", "aac":
				*warnings = append(*warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions",
					Details: fmt.Sprintf(`Encoding "%s" does not support container parameter. Container "%s" was ignored.`, newEncoding, opts.Container),
				})
				delete(query, "container")
			}
		} else {
			switch newEncoding {
			case "mp3", "flac", "aac":
				delete(query, "container")
			case "linear16", "mulaw", "alaw":
				if _, ok := query["container"]; !ok {
					query["container"] = "wav"
				}
			case "opus":
				query["container"] = "ogg"
			}
		}

		if newEncoding == "mp3" || newEncoding == "opus" || newEncoding == "aac" {
			delete(query, "sample_rate")
		}
		if newEncoding == "linear16" || newEncoding == "mulaw" || newEncoding == "alaw" || newEncoding == "flac" {
			delete(query, "bit_rate")
		}
	} else if opts.Container != "" {
		container := strings.ToLower(opts.Container)
		oldEncoding := strings.ToLower(query["encoding"])
		var newEncoding string
		switch container {
		case "wav":
			query["container"] = "wav"
			newEncoding = "linear16"
		case "ogg":
			query["container"] = "ogg"
			newEncoding = "opus"
		case "none":
			query["container"] = "none"
			newEncoding = "linear16"
		}
		if newEncoding != "" && newEncoding != oldEncoding {
			query["encoding"] = newEncoding
			if newEncoding == "mp3" || newEncoding == "opus" || newEncoding == "aac" {
				delete(query, "sample_rate")
			}
			if newEncoding == "linear16" || newEncoding == "mulaw" || newEncoding == "alaw" || newEncoding == "flac" {
				delete(query, "bit_rate")
			}
		}
	}

	if opts.SampleRate != nil {
		encoding := strings.ToLower(query["encoding"])
		sampleRate := *opts.SampleRate
		switch encoding {
		case "linear16":
			if !intInSlice([]int{8000, 16000, 24000, 32000, 48000}, sampleRate) {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "linear16" only supports sample rates: 8000, 16000, 24000, 32000, 48000. Sample rate %d was ignored.`, sampleRate)})
			} else {
				query["sample_rate"] = strconv.Itoa(sampleRate)
			}
		case "mulaw", "alaw":
			if !intInSlice([]int{8000, 16000}, sampleRate) {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "%s" only supports sample rates: 8000, 16000. Sample rate %d was ignored.`, encoding, sampleRate)})
			} else {
				query["sample_rate"] = strconv.Itoa(sampleRate)
			}
		case "flac":
			if !intInSlice([]int{8000, 16000, 22050, 32000, 48000}, sampleRate) {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "flac" only supports sample rates: 8000, 16000, 22050, 32000, 48000. Sample rate %d was ignored.`, sampleRate)})
			} else {
				query["sample_rate"] = strconv.Itoa(sampleRate)
			}
		case "mp3", "opus", "aac":
			*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "%s" has a fixed sample rate and does not support sample_rate parameter. Sample rate %d was ignored.`, encoding, sampleRate)})
		default:
			query["sample_rate"] = strconv.Itoa(sampleRate)
		}
	}

	if opts.BitRate != nil {
		encoding := strings.ToLower(query["encoding"])
		bitRateStr := fmt.Sprint(opts.BitRate)
		bitRateNum, _ := strconv.ParseFloat(bitRateStr, 64)
		switch encoding {
		case "mp3":
			if bitRateNum != 32000 && bitRateNum != 48000 {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "mp3" only supports bit rates: 32000, 48000. Bit rate %s was ignored.`, bitRateStr)})
			} else {
				query["bit_rate"] = bitRateStr
			}
		case "opus":
			if bitRateNum < 4000 || bitRateNum > 650000 {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "opus" supports bit rates between 4000 and 650000. Bit rate %s was ignored.`, bitRateStr)})
			} else {
				query["bit_rate"] = bitRateStr
			}
		case "aac":
			if bitRateNum < 4000 || bitRateNum > 192000 {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "aac" supports bit rates between 4000 and 192000. Bit rate %s was ignored.`, bitRateStr)})
			} else {
				query["bit_rate"] = bitRateStr
			}
		case "linear16", "mulaw", "alaw", "flac":
			*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "%s" does not support bit_rate parameter. Bit rate %s was ignored.`, encoding, bitRateStr)})
		default:
			query["bit_rate"] = bitRateStr
		}
	}

	if opts.Callback != "" {
		query["callback"] = opts.Callback
	}
	if opts.CallbackMethod != "" {
		query["callback_method"] = opts.CallbackMethod
	}
	if opts.MipOptOut != nil {
		query["mip_opt_out"] = strconv.FormatBool(*opts.MipOptOut)
	}
	if opts.Tag != nil {
		switch tag := opts.Tag.(type) {
		case []interface{}:
			parts := make([]string, 0, len(tag))
			for _, t := range tag {
				parts = append(parts, fmt.Sprint(t))
			}
			query["tag"] = strings.Join(parts, ",")
		case []string:
			query["tag"] = strings.Join(tag, ",")
		default:
			query["tag"] = fmt.Sprint(tag)
		}
	}
}

// buildSpeechProviderMetadata extracts Deepgram TTS response headers into the
// providerMetadata.deepgram shape (dg-project-id deliberately excluded: an
// account identifier).
func buildSpeechProviderMetadata(headers http.Header) map[string]interface{} {
	metadata := map[string]interface{}{}
	if v := headers.Get("Dg-Model-Name"); v != "" {
		metadata["modelName"] = v
	}
	if v := headers.Get("Dg-Model-Uuid"); v != "" {
		metadata["modelUuid"] = v
	}
	if v := headers.Get("Dg-Additional-Model-Uuids"); v != "" {
		metadata["additionalModelUuids"] = strings.Split(v, ",")
	}
	if v := headerInt(headers, "Dg-Char-Count"); v != nil {
		metadata["charCount"] = *v
	}
	if v := headerInt(headers, "Dg-Breaks-Applied"); v != nil {
		metadata["breaksApplied"] = *v
	}
	if v := headerInt(headers, "Dg-Pronunciations-Applied"); v != nil {
		metadata["pronunciationsApplied"] = *v
	}
	if v := headers.Get("Dg-Pronunciation-Warnings"); v != "" {
		metadata["pronunciationWarnings"] = v
	}
	if v := headers.Get("Dg-Request-Id"); v != "" {
		metadata["requestId"] = v
	}
	return metadata
}

func headerInt(headers http.Header, name string) *int {
	v := headers.Get(name)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}
