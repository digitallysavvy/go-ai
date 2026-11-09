package cartesia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements provider.SpeechModel for Cartesia
// (POST /tts/bytes).
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "cartesia.speech" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

// outputFormatSpec mirrors one entry of the TypeScript SDK's OUTPUT_FORMATS
// table (plus DEFAULT_OUTPUT_FORMAT).
type outputFormatSpec struct {
	Container  string
	Encoding   string
	SampleRate int
	BitRate    int
}

var defaultOutputFormat = outputFormatSpec{Container: "mp3", SampleRate: 44100, BitRate: 128000}

var namedOutputFormats = map[string]outputFormatSpec{
	"alaw":  {Container: "raw", Encoding: "pcm_alaw", SampleRate: 8000},
	"mp3":   defaultOutputFormat,
	"mulaw": {Container: "raw", Encoding: "pcm_mulaw", SampleRate: 8000},
	"pcm":   {Container: "raw", Encoding: "pcm_f32le", SampleRate: 44100},
	"raw":   {Container: "raw", Encoding: "pcm_f32le", SampleRate: 44100},
	"wav":   {Container: "wav", Encoding: "pcm_s16le", SampleRate: 44100},
}

var cartesiaSampleRates = map[int]bool{8000: true, 16000: true, 22050: true, 24000: true, 44100: true, 48000: true}

// resolveOutputFormat mirrors the TypeScript SDK's resolveOutputFormat.
func resolveOutputFormat(outputFormat string, opts *SpeechModelOptions, warnings *[]types.Warning) map[string]interface{} {
	if outputFormat == "" {
		outputFormat = "mp3"
	}
	parts := strings.Split(providerutils.JSToLower(outputFormat), "_")
	formatName := parts[0]
	// TS destructures `[formatName, sampleRateText, ...extraParts]`: a
	// trailing underscore ("mp3_") gives an empty, but present, sample rate.
	var sampleRateText string
	hasSampleRate := len(parts) > 1
	var extraParts []string
	if hasSampleRate {
		sampleRateText = parts[1]
		extraParts = parts[2:]
	}

	mapped, hasMapped := namedOutputFormats[formatName]
	resolved := defaultOutputFormat
	if hasMapped {
		resolved = mapped
	}

	if !hasMapped {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unknown output format \"%s\". Falling back to mp3. Use providerOptions.cartesia to configure container, encoding, and sampleRate directly.", outputFormat),
		})
	} else if hasSampleRate {
		// TS Number(sampleRateText) + Number.isInteger + the rate list.
		parsedRate, ok := providerutils.JSNumber(sampleRateText)
		if len(extraParts) == 0 && ok && parsedRate == math.Trunc(parsedRate) && cartesiaSampleRates[int(parsedRate)] {
			resolved.SampleRate = int(parsedRate)
		} else {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported Cartesia sample rate in output format \"%s\". Using %d Hz instead.", outputFormat, resolved.SampleRate),
			})
		}
	}

	container := resolved.Container
	if opts != nil && opts.Container != "" {
		container = opts.Container
	}
	sampleRate := resolved.SampleRate
	if opts != nil && opts.SampleRate != nil {
		sampleRate = *opts.SampleRate
	}

	if container == "mp3" {
		if opts != nil && opts.Encoding != "" {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.cartesia.encoding",
				Details: "Cartesia MP3 output does not accept an encoding. The encoding option was ignored.",
			})
		}
		bitRate := 128000
		if resolved.Container == "mp3" {
			bitRate = resolved.BitRate
		}
		if opts != nil && opts.BitRate != nil {
			bitRate = *opts.BitRate
		}
		return map[string]interface{}{"container": container, "sample_rate": sampleRate, "bit_rate": bitRate}
	}

	if opts != nil && opts.BitRate != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.cartesia.bitRate",
			Details: "Cartesia raw and WAV output do not accept a bit rate. The bitRate option was ignored.",
		})
	}

	encoding := resolved.Encoding
	if resolved.Container == "mp3" {
		if container == "wav" {
			encoding = "pcm_s16le"
		} else {
			encoding = "pcm_f32le"
		}
	}
	if opts != nil && opts.Encoding != "" {
		encoding = opts.Encoding
	}

	return map[string]interface{}{"container": container, "encoding": encoding, "sample_rate": sampleRate}
}

// cartesiaSpeechBodyOrder is the key order of the TS requestBody literal
// (cartesia-speech-model.ts) and its output_format object, which
// JSON.stringify keeps.
var cartesiaSpeechBodyOrder = map[string][]string{
	"":                  {"model_id", "transcript", "voice", "output_format", "locale", "language", "normalization", "pronunciation_dict_id", "generation_config"},
	"voice":             {"mode", "id"},
	"output_format":     {"container", "encoding", "sample_rate", "bit_rate"},
	"generation_config": {"speed", "volume", "emotion"},
}

// DoGenerate performs text-to-speech synthesis.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	// TS getArgs parses the provider options before requiring a voice.
	if err := validateSpeechOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	if opts.Voice == "" {
		return nil, errors.New("Cartesia speech models require a `voice` to be set.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	currentDate := time.Now()

	requestBody, warnings, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}
	requestBytes := []byte(providerutils.StringifyOrdered(requestBody, cartesiaSpeechBodyOrder))

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/tts/bytes",
		Body:    requestBytes,
		Headers: opts.Headers,
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
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}
	cartesiaOpts := extractSpeechModelOptions(opts.ProviderOptions)
	rawOpts := rawSpeechOptions(opts.ProviderOptions)

	outputFormat := resolveOutputFormat(opts.OutputFormat, cartesiaOpts, &warnings)

	body := map[string]interface{}{
		"model_id":   m.modelID,
		"transcript": opts.Text,
		"voice": map[string]interface{}{
			"mode": "id",
			"id":   opts.Voice,
		},
		"output_format": outputFormat,
	}

	// TS 47b666b6c7: locale and the provider language are exclusive
	// (`!= null`, so "" counts); a locale wins over the generic language
	// with a warning; otherwise the provider language, then the generic one.
	locale, hasLocale := rawOpts["locale"].(string)
	lang, hasLang := speechLanguageOption(opts.ProviderOptions, cartesiaOpts)
	if hasLocale && hasLang {
		return nil, nil, providererrors.NewArgumentError("providerOptions",
			"providerOptions.cartesia.locale cannot be combined with providerOptions.cartesia.language", nil)
	}
	switch {
	case hasLocale:
		body["locale"] = locale
		if opts.Language != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "language",
				Details: "Cartesia does not support both locale and language. The language option was ignored in favor of providerOptions.cartesia.locale.",
			})
		}
	case hasLang:
		body["language"] = lang
	case opts.Language != "":
		body["language"] = opts.Language
	}

	if v, ok := rawOpts["normalization"].(string); ok {
		body["normalization"] = v
	}
	if v, ok := rawOpts["pronunciationDictionaryId"].(string); ok {
		body["pronunciation_dict_id"] = v
	}

	// TS 392d2fa3a4: generation_config collects speed, volume and emotion
	// and is sent only when non-empty.
	generationConfig := map[string]interface{}{}
	resolvedSpeed := opts.Speed
	if cartesiaOpts != nil && cartesiaOpts.Speed != nil {
		resolvedSpeed = cartesiaOpts.Speed
	}
	if resolvedSpeed != nil {
		if *resolvedSpeed >= 0.6 && *resolvedSpeed <= 1.5 {
			generationConfig["speed"] = *resolvedSpeed
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "speed",
				Details: "Cartesia speed must be between 0.6 and 1.5. The speed option was ignored.",
			})
		}
	}
	if v, ok := rawOpts["volume"]; ok && v != nil {
		generationConfig["volume"] = v
	}
	if v, ok := rawOpts["emotion"].(string); ok {
		generationConfig["emotion"] = v
	}
	if len(generationConfig) > 0 {
		body["generation_config"] = generationConfig
	}

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Cartesia speech models do not support instructions. Instructions parameter was ignored.",
		})
	}

	return body, warnings, nil
}

// rawSpeechOptions returns providerOptions["cartesia"] as a JSON object
// (a typed SpeechModelOptions value through its JSON form), so option
// presence follows TS `!= null`.
func rawSpeechOptions(providerOptions map[string]interface{}) map[string]interface{} {
	raw, ok := providerOptions["cartesia"]
	if !ok || raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]interface{}); ok {
		return m
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// speechLanguageOption reports providerOptions.cartesia.language and whether
// it is present and a string (the typed options struct cannot tell "" from
// unset, so a map-shaped option is consulted directly).
func speechLanguageOption(providerOptions map[string]interface{}, parsed *SpeechModelOptions) (string, bool) {
	if parsed != nil && parsed.Language != "" {
		return parsed.Language, true
	}
	raw, ok := providerOptions["cartesia"]
	if !ok || raw == nil {
		return "", false
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		b, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(b, &m) != nil {
			return "", false
		}
	}
	lang, ok := m["language"].(string)
	return lang, ok
}
