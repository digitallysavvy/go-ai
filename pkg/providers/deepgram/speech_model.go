package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
func (m *SpeechModel) Provider() string { return "deepgram.speech" }

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
	var rate struct {
		SampleRate *float64 `json:"sampleRate"`
	}
	if json.Unmarshal(b, &rate) == nil {
		opts.sampleRate = rate.SampleRate
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		// A field the Go struct can't hold (TS sampleRate is z.number(), the
		// struct an int) must not drop every other option: encoding/json
		// leaves the offending field unset and fills the rest.
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil
		}
	}
	return &opts
}

// DoGenerate performs speech synthesis via Deepgram's REST TTS endpoint.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()

	if err := validateSpeechOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
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

	// JSON.stringify leaves <, > and & literal.
	requestBytes := []byte(providerutils.StringifyOrdered(requestBody, nil))

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
		},
	}
	// TS: `...(charCount != null ? { usage: { characters: charCount } } : {})`.
	if charCount := headerNumber(resp.Headers, "Dg-Char-Count"); charCount != nil {
		result.Usage = map[string]interface{}{"characters": charCount}
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
		trimmedVoice := providerutils.JSTrim(opts.Voice)
		if trimmedVoice == "" {
			return nil, nil, fmt.Errorf("Deepgram speech model \"%s\" requires a `voice` to be set (e.g. voice: 'thalia').", m.modelID) //nolint:staticcheck // matches TS SDK's exact error text
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

	// TS: outputFormat = 'mp3' default.
	outputFormat := opts.OutputFormat
	if outputFormat == "" {
		outputFormat = "mp3"
	}
	applyOutputFormat(query, outputFormat)

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
		query["speed"] = providerutils.JSNumberString(*opts.Speed)
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
	parts := strings.Split(formatLower, "_")
	if len(parts) < 2 {
		return
	}
	firstPart, secondPart := parts[0], parts[1]
	sampleRate, sampleRateErr := jsParseInt(secondPart)

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
			validRates := map[string][]float64{
				"linear16": {8000, 16000, 24000, 32000, 48000},
				"mulaw":    {8000, 16000},
				"alaw":     {8000, 16000},
				"flac":     {8000, 16000, 22050, 32000, 48000},
			}
			if rates, ok := validRates[firstPart]; ok && inRates(rates, sampleRate) {
				query["sample_rate"] = providerutils.JSNumberString(sampleRate)
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
			query["sample_rate"] = providerutils.JSNumberString(sampleRate)
		}
	}
}

// jsParseInt is JS parseInt(s, 10): the leading integer of s (arbitrarily
// large values become the nearest double, as in JS), an error (NaN) when s does
// not start with one.
func jsParseInt(s string) (float64, error) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	digits := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == digits {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return strconv.ParseFloat(s[:end], 64)
}

func inRates(rates []float64, v float64) bool {
	for _, x := range rates {
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

	sampleRatePtr := opts.sampleRate
	if sampleRatePtr == nil && opts.SampleRate != nil {
		v := float64(*opts.SampleRate)
		sampleRatePtr = &v
	}
	if sampleRatePtr != nil {
		encoding := strings.ToLower(query["encoding"])
		sampleRate := *sampleRatePtr
		// JS String(number): 22050.5 stays fractional, 48000 has no ".0".
		rateStr := providerutils.JSNumberString(sampleRate)
		inRates := func(rates ...float64) bool {
			for _, r := range rates {
				if r == sampleRate {
					return true
				}
			}
			return false
		}
		unsupported := func(details string) {
			*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: details})
		}
		switch encoding {
		case "linear16":
			if !inRates(8000, 16000, 24000, 32000, 48000) {
				unsupported(fmt.Sprintf(`Encoding "linear16" only supports sample rates: 8000, 16000, 24000, 32000, 48000. Sample rate %s was ignored.`, rateStr))
			} else {
				query["sample_rate"] = rateStr
			}
		case "mulaw", "alaw":
			if !inRates(8000, 16000) {
				unsupported(fmt.Sprintf(`Encoding "%s" only supports sample rates: 8000, 16000. Sample rate %s was ignored.`, encoding, rateStr))
			} else {
				query["sample_rate"] = rateStr
			}
		case "flac":
			if !inRates(8000, 16000, 22050, 32000, 48000) {
				unsupported(fmt.Sprintf(`Encoding "flac" only supports sample rates: 8000, 16000, 22050, 32000, 48000. Sample rate %s was ignored.`, rateStr))
			} else {
				query["sample_rate"] = rateStr
			}
		case "mp3", "opus", "aac":
			unsupported(fmt.Sprintf(`Encoding "%s" has a fixed sample rate and does not support sample_rate parameter. Sample rate %s was ignored.`, encoding, rateStr))
		default:
			query["sample_rate"] = rateStr
		}
	}

	if opts.BitRate != nil {
		encoding := strings.ToLower(query["encoding"])
		bitRateStr := jsStringOf(opts.BitRate)
		// TS Number(bitRate): a non-numeric string is NaN, so every range
		// comparison is false (opus/aac accept it) and mp3's list check fails.
		bitRateNum, numOK := providerutils.JSNumber(bitRateStr)
		if !numOK {
			bitRateNum = math.NaN()
		}
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
		case string:
			// TS `if (deepgramOptions.tag)`: an empty string is falsy.
			if tag != "" {
				query["tag"] = tag
			}
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
	if vals := headers.Values("Dg-Additional-Model-Uuids"); len(vals) > 0 {
		metadata["additionalModelUuids"] = strings.Split(vals[0], ",")
	}
	if v := headerNumber(headers, "Dg-Char-Count"); v != nil {
		metadata["charCount"] = v
	}
	if v := headerNumber(headers, "Dg-Breaks-Applied"); v != nil {
		metadata["breaksApplied"] = v
	}
	if v := headerNumber(headers, "Dg-Pronunciations-Applied"); v != nil {
		metadata["pronunciationsApplied"] = v
	}
	if v := headers.Get("Dg-Pronunciation-Warnings"); v != "" {
		metadata["pronunciationWarnings"] = v
	}
	if v := headers.Get("Dg-Request-Id"); v != "" {
		metadata["requestId"] = v
	}
	return metadata
}

// headerNumber is the TS headerNumber helper: absent -> nil, otherwise
// Number(value) (an empty header is 0), NaN -> nil. Integral values stay int.
func headerNumber(headers http.Header, name string) interface{} {
	vals := headers.Values(name)
	if len(vals) == 0 {
		return nil
	}
	f, ok := providerutils.JSNumber(vals[0])
	if !ok || math.IsNaN(f) {
		return nil
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return int(f)
	}
	return f
}

// jsStringOf is JS String(value) for a decoded JSON scalar (bitRate is
// number | string).
func jsStringOf(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return providerutils.JSNumberString(n)
	case float32:
		return providerutils.JSNumberString(float64(n))
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return providerutils.JSNumberString(f)
		}
	}
	return fmt.Sprint(v)
}
