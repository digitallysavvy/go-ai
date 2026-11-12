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
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
		Method:       http.MethodPost,
		Path:         "/v1/speak",
		Body:         requestBody,
		SearchParams: queryParams.searchParams(),
		Headers:      opts.Headers,
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

func (m *SpeechModel) buildArgs(opts *provider.SpeechGenerateOptions) (*dgQuery, []types.Warning, error) {
	warnings := []types.Warning{}
	dgOpts := extractSpeechModelOptions(opts.ProviderOptions)

	// TS 5cc6dcaa65: with a callback URL Deepgram answers with a JSON
	// acknowledgement instead of audio, so the call fails before any
	// request; callbackMethod alone is ignored with a warning. (An empty
	// string never gets here: the option schema rejects it.)
	if dgOpts != nil && dgOpts.Callback != "" {
		return nil, nil, providererrors.NewArgumentError("providerOptions.deepgram.callback",
			"Deepgram processes speech requests with a `callback` URL asynchronously: it returns a JSON acknowledgement instead of audio and delivers the audio to the callback URL. `generateSpeech` requires the audio response, so `providerOptions.deepgram.callback` is not supported. Remove the option to receive the audio, or call the Deepgram REST API directly when you need asynchronous callback delivery.", nil)
	}
	if dgOpts != nil && dgOpts.CallbackMethod != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.deepgram.callbackMethod",
			Details: "providerOptions.deepgram.callbackMethod is only used together with `callback`, which is not supported by the AI SDK. Callback method was ignored.",
		})
	}

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

	// TS queryParams is a JS object: keys keep their insertion order on the
	// wire, a deleted key that is set again moves to the end.
	query := newDGQuery()
	query.set("model", upstreamModelID)

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
		query.set("speed", providerutils.JSNumberString(*opts.Speed))
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
func applyOutputFormat(query *dgQuery, outputFormat string) {
	formatLower := providerutils.JSToLower(outputFormat)

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
			query.set("encoding", mf.encoding)
		}
		if mf.container != "" {
			query.set("container", mf.container)
		}
		if mf.sampleRate != 0 {
			query.set("sample_rate", strconv.Itoa(mf.sampleRate))
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
		query.set("encoding", firstPart)
		switch firstPart {
		case "linear16", "mulaw", "alaw":
			query.set("container", "wav")
		case "opus":
			query.set("container", "ogg")
		}
		if sampleRateErr == nil {
			validRates := map[string][]float64{
				"linear16": {8000, 16000, 24000, 32000, 48000},
				"mulaw":    {8000, 16000},
				"alaw":     {8000, 16000},
				"flac":     {8000, 16000, 22050, 32000, 48000},
			}
			if rates, ok := validRates[firstPart]; ok && inRates(rates, sampleRate) {
				query.set("sample_rate", providerutils.JSNumberString(sampleRate))
			}
		}
	case "wav", "ogg":
		if firstPart == "wav" {
			query.set("container", "wav")
			query.set("encoding", "linear16")
		} else {
			query.set("container", "ogg")
			query.set("encoding", "opus")
		}
		if sampleRateErr == nil {
			query.set("sample_rate", providerutils.JSNumberString(sampleRate))
		}
	}
}

// jsParseInt is JS parseInt(s, 10): the leading integer of s (arbitrarily
// large values become the nearest double, as in JS), an error (NaN) when s does
// not start with one.
func jsParseInt(s string) (float64, error) {
	s = providerutils.JSTrimStart(s) // JS StrWhiteSpaceChar (NBSP, BOM, ... included)
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
func applyDeepgramSpeechOptions(query *dgQuery, opts *SpeechModelOptions, warnings *[]types.Warning) {
	if opts.Encoding != "" {
		newEncoding := providerutils.JSToLower(opts.Encoding)
		query.set("encoding", newEncoding)

		if opts.Container != "" {
			container := providerutils.JSToLower(opts.Container)
			switch newEncoding {
			case "linear16", "mulaw", "alaw":
				if container != "wav" && container != "none" {
					*warnings = append(*warnings, types.Warning{
						Type:    "unsupported",
						Feature: "providerOptions",
						Details: fmt.Sprintf(`Encoding "%s" only supports containers "wav" or "none". Container "%s" was ignored.`, newEncoding, opts.Container),
					})
				} else {
					query.set("container", container)
				}
			case "opus":
				query.set("container", "ogg")
			case "mp3", "flac", "aac":
				*warnings = append(*warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions",
					Details: fmt.Sprintf(`Encoding "%s" does not support container parameter. Container "%s" was ignored.`, newEncoding, opts.Container),
				})
				query.del("container")
			}
		} else {
			switch newEncoding {
			case "mp3", "flac", "aac":
				query.del("container")
			case "linear16", "mulaw", "alaw":
				if !query.has("container") {
					query.set("container", "wav")
				}
			case "opus":
				query.set("container", "ogg")
			}
		}

		if newEncoding == "mp3" || newEncoding == "opus" || newEncoding == "aac" {
			query.del("sample_rate")
		}
		if newEncoding == "linear16" || newEncoding == "mulaw" || newEncoding == "alaw" || newEncoding == "flac" {
			query.del("bit_rate")
		}
	} else if opts.Container != "" {
		container := providerutils.JSToLower(opts.Container)
		oldEncoding := providerutils.JSToLower(query.get("encoding"))
		var newEncoding string
		switch container {
		case "wav":
			query.set("container", "wav")
			newEncoding = "linear16"
		case "ogg":
			query.set("container", "ogg")
			newEncoding = "opus"
		case "none":
			query.set("container", "none")
			newEncoding = "linear16"
		}
		if newEncoding != "" && newEncoding != oldEncoding {
			query.set("encoding", newEncoding)
			if newEncoding == "mp3" || newEncoding == "opus" || newEncoding == "aac" {
				query.del("sample_rate")
			}
			if newEncoding == "linear16" || newEncoding == "mulaw" || newEncoding == "alaw" || newEncoding == "flac" {
				query.del("bit_rate")
			}
		}
	}

	sampleRatePtr := opts.sampleRate
	if sampleRatePtr == nil && opts.SampleRate != nil {
		v := float64(*opts.SampleRate)
		sampleRatePtr = &v
	}
	if sampleRatePtr != nil {
		encoding := providerutils.JSToLower(query.get("encoding"))
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
				query.set("sample_rate", rateStr)
			}
		case "mulaw", "alaw":
			if !inRates(8000, 16000) {
				unsupported(fmt.Sprintf(`Encoding "%s" only supports sample rates: 8000, 16000. Sample rate %s was ignored.`, encoding, rateStr))
			} else {
				query.set("sample_rate", rateStr)
			}
		case "flac":
			if !inRates(8000, 16000, 22050, 32000, 48000) {
				unsupported(fmt.Sprintf(`Encoding "flac" only supports sample rates: 8000, 16000, 22050, 32000, 48000. Sample rate %s was ignored.`, rateStr))
			} else {
				query.set("sample_rate", rateStr)
			}
		case "mp3", "opus", "aac":
			unsupported(fmt.Sprintf(`Encoding "%s" has a fixed sample rate and does not support sample_rate parameter. Sample rate %s was ignored.`, encoding, rateStr))
		default:
			query.set("sample_rate", rateStr)
		}
	}

	if opts.BitRate != nil {
		encoding := providerutils.JSToLower(query.get("encoding"))
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
				query.set("bit_rate", bitRateStr)
			}
		case "opus":
			if bitRateNum < 4000 || bitRateNum > 650000 {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "opus" supports bit rates between 4000 and 650000. Bit rate %s was ignored.`, bitRateStr)})
			} else {
				query.set("bit_rate", bitRateStr)
			}
		case "aac":
			if bitRateNum < 4000 || bitRateNum > 192000 {
				*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "aac" supports bit rates between 4000 and 192000. Bit rate %s was ignored.`, bitRateStr)})
			} else {
				query.set("bit_rate", bitRateStr)
			}
		case "linear16", "mulaw", "alaw", "flac":
			*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "providerOptions", Details: fmt.Sprintf(`Encoding "%s" does not support bit_rate parameter. Bit rate %s was ignored.`, encoding, bitRateStr)})
		default:
			query.set("bit_rate", bitRateStr)
		}
	}

	if opts.MipOptOut != nil {
		query.set("mip_opt_out", strconv.FormatBool(*opts.MipOptOut))
	}
	if opts.Tag != nil {
		switch tag := opts.Tag.(type) {
		case []interface{}:
			parts := make([]string, 0, len(tag))
			for _, t := range tag {
				parts = append(parts, fmt.Sprint(t))
			}
			query.set("tag", strings.Join(parts, ","))
		case []string:
			query.set("tag", strings.Join(tag, ","))
		case string:
			// TS `if (deepgramOptions.tag)`: an empty string is falsy.
			if tag != "" {
				query.set("tag", tag)
			}
		default:
			query.set("tag", fmt.Sprint(tag))
		}
	}
}

// buildSpeechProviderMetadata extracts Deepgram TTS response headers into the
// providerMetadata.deepgram shape (dg-project-id deliberately excluded: an
// account identifier).
func buildSpeechProviderMetadata(headers http.Header) map[string]interface{} {
	metadata := map[string]interface{}{}
	if v := providerutils.FetchHeaderText(headers, "Dg-Model-Name"); v != "" {
		metadata["modelName"] = v
	}
	if v := providerutils.FetchHeaderText(headers, "Dg-Model-Uuid"); v != "" {
		metadata["modelUuid"] = v
	}
	if len(headers.Values("Dg-Additional-Model-Uuids")) > 0 {
		metadata["additionalModelUuids"] = strings.Split(providerutils.FetchHeaderText(headers, "Dg-Additional-Model-Uuids"), ",")
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
	if v := providerutils.FetchHeaderText(headers, "Dg-Pronunciation-Warnings"); v != "" {
		metadata["pronunciationWarnings"] = v
	}
	if v := providerutils.FetchHeaderText(headers, "Dg-Request-Id"); v != "" {
		metadata["requestId"] = v
	}
	return metadata
}

// headerNumber is the TS headerNumber helper: absent -> nil, otherwise
// Number(value) (an empty header is 0), NaN -> nil. Integral values stay int.
func headerNumber(headers http.Header, name string) interface{} {
	if len(headers.Values(name)) == 0 {
		return nil
	}
	f, ok := providerutils.JSNumber(providerutils.FetchHeaderText(headers, name))
	if !ok || math.IsNaN(f) {
		return nil
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		// int keeps the v0.5.2 type; outside the platform int range (32-bit
		// builds) the value is an int64 instead of a corrupted int.
		if f >= math.MinInt && f <= math.MaxInt {
			return int(f)
		}
		return int64(f)
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

// dgQuery is a JS object of query parameters (TS getArgs queryParams): set
// keeps an existing key's position and appends a new one, del removes a key,
// and searchParams sends them in that order (URLSearchParams over
// Object.entries).
type dgQuery struct {
	keys []string
	vals map[string]string
}

func newDGQuery() *dgQuery { return &dgQuery{vals: map[string]string{}} }

func (q *dgQuery) set(key, value string) {
	if _, ok := q.vals[key]; !ok {
		q.keys = append(q.keys, key)
	}
	q.vals[key] = value
}

func (q *dgQuery) get(key string) string { return q.vals[key] }

func (q *dgQuery) has(key string) bool {
	_, ok := q.vals[key]
	return ok
}

func (q *dgQuery) del(key string) {
	if !q.has(key) {
		return
	}
	delete(q.vals, key)
	for i, k := range q.keys {
		if k == key {
			q.keys = append(q.keys[:i:i], q.keys[i+1:]...)
			break
		}
	}
}

func (q *dgQuery) searchParams() *internalhttp.SearchParams {
	p := internalhttp.NewSearchParams()
	for _, k := range q.keys {
		p.Append(k, q.vals[k])
	}
	return p
}
