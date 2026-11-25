package assemblyai

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements provider.SpeechModel for AssemblyAI text-to-speech
// (POST {speech host}/v1/speech). Mirrors TS AssemblyAISpeechModel.
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates an AssemblyAI speech model.
func NewSpeechModel(p *Provider, modelID string) *SpeechModel {
	return &SpeechModel{provider: p, modelID: modelID}
}

var _ provider.SpeechModel = (*SpeechModel)(nil)

// SpecificationVersion returns "v4".
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns "assemblyai.speech".
func (m *SpeechModel) Provider() string { return "assemblyai.speech" }

// ModelID returns the model ID.
func (m *SpeechModel) ModelID() string { return m.modelID }

// speechFormat is the encoding, container and sample rate an output format
// implies (TS OUTPUT_FORMATS).
type speechFormat struct {
	encoding   string
	container  string
	sampleRate int
}

var speechOutputFormats = map[string]speechFormat{
	"alaw":  {encoding: "pcm_alaw", container: "raw", sampleRate: 8000},
	"mp3":   {encoding: "mp3"},
	"mulaw": {encoding: "pcm_mulaw", container: "raw", sampleRate: 8000},
	"pcm":   {encoding: "pcm_s16le", container: "raw"},
	"wav":   {encoding: "pcm_s16le", container: "wav"},
}

// speechLanguages translates ISO 639-1 codes to the API's language names (TS
// LANGUAGES).
var speechLanguages = map[string]string{
	"de": "german", "en": "english", "es": "spanish", "fr": "french", "it": "italian", "pt": "portuguese",
}

// speechBodyOrder is the TS requestBody key order: the literal, then the
// assignments in getArgs.
var speechBodyOrder = map[string][]string{
	"": {"text", "model_id", "encoding", "stream", "voice_id", "container", "sample_rate", "bit_rate", "language"},
}

// buildSpeechBody is TS getArgs: the request body and the warnings.
func (m *SpeechModel) buildSpeechBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}
	aaiOpts, err := parseSpeechOptions(opts.ProviderOptions)
	if err != nil {
		return nil, nil, err
	}
	if aaiOpts == nil {
		aaiOpts = &SpeechModelOptions{}
	}

	outputFormat := opts.OutputFormat
	if outputFormat == "" {
		outputFormat = "mp3"
	}
	format, known := speechOutputFormats[strings.ToLower(outputFormat)]
	if !known {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unknown output format \"%s\". Falling back to mp3. Use providerOptions.assemblyai to configure encoding, container, and sampleRate directly.", outputFormat),
		})
		format = speechOutputFormats["mp3"]
	}

	encoding := format.encoding
	if aaiOpts.Encoding != "" {
		encoding = aaiOpts.Encoding
	}
	// Container and sample rate implied by outputFormat only apply to the
	// encoding they were derived for.
	formatApplies := encoding == format.encoding

	body := map[string]interface{}{
		"text":     opts.Text,
		"model_id": m.modelID,
		"encoding": encoding,
		// Buffered responses report the final billed character count and are
		// never cut mid-body by an upstream failure.
		"stream": false,
	}
	if opts.Voice != "" {
		body["voice_id"] = opts.Voice
	}

	container := aaiOpts.Container
	if container == "" && formatApplies {
		container = format.container
	}
	if container != "" {
		if encoding == "mp3" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.assemblyai.container",
				Details: "AssemblyAI MP3 output has no container. The container option was ignored.",
			})
		} else {
			body["container"] = container
		}
	}

	if aaiOpts.SampleRate != nil {
		body["sample_rate"] = *aaiOpts.SampleRate
	} else if formatApplies && format.sampleRate != 0 {
		body["sample_rate"] = format.sampleRate
	}

	if aaiOpts.BitRate != nil {
		if encoding == "mp3" {
			body["bit_rate"] = *aaiOpts.BitRate
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.assemblyai.bitRate",
				Details: "AssemblyAI only accepts a bit rate for MP3 output. The bitRate option was ignored.",
			})
		}
	}

	if opts.Language != "" {
		normalized := strings.ToLower(opts.Language)
		mapped, ok := speechLanguages[normalized]
		if !ok {
			for _, name := range speechLanguages {
				if name == normalized {
					mapped, ok = name, true
				}
			}
		}
		if ok {
			body["language"] = mapped
		} else {
			codes := make([]string, 0, len(speechLanguages))
			for code := range speechLanguages {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			details := fmt.Sprintf("AssemblyAI text-to-speech does not support language \"%s\" (supported: %s). The language option was ignored.", opts.Language, strings.Join(codes, ", "))
			if normalized == "auto" {
				details = "AssemblyAI text-to-speech does not detect the language. The voice speaks its own language."
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "language", Details: details})
		}
	}

	if opts.Speed != nil && *opts.Speed != 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "speed",
			Details: "AssemblyAI text-to-speech only supports the default speed. The speed option was ignored.",
		})
	}
	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "AssemblyAI text-to-speech does not support instructions. Instructions parameter was ignored.",
		})
	}
	return body, warnings, nil
}

// DoGenerate synthesizes speech from text.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()
	body, warnings, err := m.buildSpeechBody(opts)
	if err != nil {
		return nil, err
	}
	requestBody := providerutils.StringifyOrdered(body, speechBodyOrder)

	resp, err := m.provider.speechClient.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/speech",
		Body:    []byte(requestBody),
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, handleError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, handleError(resp.StatusError([]byte(textdecode.UTF8(resp.Body)))) // TS reads the failed body as text
	}
	// TS createBinaryResponseHandler: a 204 / 205 has a null body.
	if err := fileutil.EmptyBinaryBodyError("assemblyai", resp.StatusCode, resp.Headers); err != nil {
		return nil, err
	}

	headers := lowerHeaders(providerutils.ExtractHeaders(resp.Headers))
	headerNumber := func(name string) (float64, bool) {
		value, ok := headers[name]
		if !ok {
			return 0, false
		}
		n, ok := jsNumber(value)
		return n, ok
	}
	billedCharacters, hasBilled := headerNumber("x-tts-billed-characters")
	audioDurationMs, hasDuration := headerNumber("x-tts-audio-duration-ms")
	sampleRate, hasSampleRate := headerNumber("x-tts-sample-rate")

	metadata := map[string]interface{}{}
	for _, field := range [][2]string{{"x-request-id", "requestId"}, {"x-tts-model", "model"}, {"x-tts-voice-id", "voiceId"}, {"x-tts-language", "language"}, {"x-tts-encoding", "encoding"}} {
		if v := headers[field[0]]; v != "" {
			metadata[field[1]] = v
		}
	}
	if hasSampleRate {
		metadata["sampleRate"] = sampleRate
	}
	if hasDuration {
		metadata["audioDurationMs"] = audioDurationMs
	}
	if hasBilled {
		metadata["billedCharacters"] = billedCharacters
	}

	result := &types.SpeechResult{
		Audio:    resp.Body,
		Warnings: warnings,
		Request:  &types.StepRequest{Body: requestBody},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   headers,
		},
		ProviderMetadata: map[string]interface{}{"assemblyai": metadata},
	}
	if hasBilled {
		result.Usage = map[string]interface{}{"characters": billedCharacters}
	}
	return result, nil
}

// jsNumber is JS Number(value) for a header value, false for NaN: surrounding
// whitespace is trimmed and an empty string is 0.
func jsNumber(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, true
	}
	n, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return n, true // ±Infinity, as Number("1e999")
		}
		return 0, false
	}
	return n, true
}

// lowerHeaders lowercases header names, as TS response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
