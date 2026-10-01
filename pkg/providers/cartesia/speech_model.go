package cartesia

import (
	"context"
	"encoding/json"
	"errors"
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
	parts := strings.Split(strings.ToLower(outputFormat), "_")
	formatName := parts[0]
	var sampleRateText string
	var extraParts []string
	if len(parts) > 1 {
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
			Details: fmt.Sprintf("Unknown output format %q. Falling back to mp3. Use providerOptions.cartesia to configure container, encoding, and sampleRate directly.", outputFormat),
		})
	} else if sampleRateText != "" {
		parsedRate, err := strconv.Atoi(sampleRateText)
		if len(extraParts) == 0 && err == nil && cartesiaSampleRates[parsedRate] {
			resolved.SampleRate = parsedRate
		} else {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported Cartesia sample rate in output format %q. Using %d Hz instead.", outputFormat, resolved.SampleRate),
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

// DoGenerate performs text-to-speech synthesis.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	if opts.Voice == "" {
		return nil, errors.New("cartesia: speech models require a `voice` to be set")
	}
	currentDate := time.Now()

	requestBody, warnings := m.buildRequestBody(opts)
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

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
			Body:      resp.Body,
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}
	cartesiaOpts := extractSpeechModelOptions(opts.ProviderOptions)

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

	if opts.Language != "" {
		body["language"] = opts.Language
	}
	// Provider-specific options override the corresponding generic options.
	if cartesiaOpts != nil && cartesiaOpts.Language != "" {
		body["language"] = cartesiaOpts.Language
	}

	resolvedSpeed := opts.Speed
	if cartesiaOpts != nil && cartesiaOpts.Speed != nil {
		resolvedSpeed = cartesiaOpts.Speed
	}
	if resolvedSpeed != nil {
		if *resolvedSpeed >= 0.6 && *resolvedSpeed <= 1.5 {
			body["generation_config"] = map[string]interface{}{"speed": *resolvedSpeed}
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "speed",
				Details: "Cartesia speed must be between 0.6 and 1.5. The speed option was ignored.",
			})
		}
	}

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Cartesia speech models do not support instructions. Instructions parameter was ignored.",
		})
	}

	return body, warnings
}
