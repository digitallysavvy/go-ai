package elevenlabs

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
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel implements the provider.SpeechModel interface for ElevenLabs
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// SpeechVoiceSettings contains ElevenLabs voice setting provider options.
type SpeechVoiceSettings struct {
	Stability       *float64
	SimilarityBoost *float64
	Style           *float64
	UseSpeakerBoost *bool
}

// PronunciationDictionaryLocator identifies an ElevenLabs pronunciation dictionary.
type PronunciationDictionaryLocator struct {
	PronunciationDictionaryID string
	VersionID                 string
}

// SpeechModelOptions contains ElevenLabs-specific speech provider options.
type SpeechModelOptions struct {
	LanguageCode                    string
	VoiceSettings                   *SpeechVoiceSettings
	PronunciationDictionaryLocators []PronunciationDictionaryLocator
	Seed                            *float64
	PreviousText                    string
	NextText                        string
	PreviousRequestIDs              []string
	NextRequestIDs                  []string
	ApplyTextNormalization          string
	ApplyLanguageTextNormalization  *bool
	EnableLogging                   *bool
}

// NewSpeechModel creates a new ElevenLabs speech synthesis model
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "elevenlabs"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody, queryParams, warnings, voice := m.buildRequestArgs(opts)
	currentDate := time.Now()

	path := fmt.Sprintf("/v1/text-to-speech/%s", voice)
	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Query:   queryParams,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError("elevenlabs", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LElevenLabs TTS API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}
	requestBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ElevenLabs speech request metadata: %w", err)
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

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) map[string]interface{} {
	body, _, _, _ := m.buildRequestArgs(opts)
	return body
}

func (m *SpeechModel) buildRequestArgs(opts *provider.SpeechGenerateOptions) (map[string]interface{}, map[string]string, []types.Warning, string) {
	voice := opts.Voice
	if voice == "" {
		voice = "21m00Tcm4TlvDq8ikWAM"
	}

	body := map[string]interface{}{
		"text":     opts.Text,
		"model_id": m.modelID,
	}
	queryParams := map[string]string{
		"output_format": "mp3_44100_128",
	}
	if opts.OutputFormat != "" {
		queryParams["output_format"] = mapElevenLabsOutputFormat(opts.OutputFormat)
	}

	voiceSettings := map[string]interface{}{}
	warnings := make([]types.Warning, 0)
	if opts.Language != "" {
		body["language_code"] = opts.Language
	}
	if opts.Speed != nil {
		voiceSettings["speed"] = *opts.Speed
	}
	if elevenLabsOptions, ok := extractSpeechModelOptions(opts.ProviderOptions); ok {
		applySpeechModelOptions(body, voiceSettings, queryParams, elevenLabsOptions)
	}
	if len(voiceSettings) > 0 {
		body["voice_settings"] = voiceSettings
	}
	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "ElevenLabs speech models do not support instructions. Instructions parameter was ignored.",
		})
	}
	return body, queryParams, warnings, voice
}

func mapElevenLabsOutputFormat(outputFormat string) string {
	switch outputFormat {
	case "mp3":
		return "mp3_44100_128"
	case "mp3_32":
		return "mp3_44100_32"
	case "mp3_64":
		return "mp3_44100_64"
	case "mp3_96":
		return "mp3_44100_96"
	case "mp3_128":
		return "mp3_44100_128"
	case "mp3_192":
		return "mp3_44100_192"
	case "pcm":
		return "pcm_44100"
	case "pcm_16000", "pcm_22050", "pcm_24000", "pcm_44100", "ulaw":
		if outputFormat == "ulaw" {
			return "ulaw_8000"
		}
		return outputFormat
	default:
		return outputFormat
	}
}

func applySpeechModelOptions(body, voiceSettings map[string]interface{}, queryParams map[string]string, opts SpeechModelOptions) {
	if opts.VoiceSettings != nil {
		if opts.VoiceSettings.Stability != nil {
			voiceSettings["stability"] = *opts.VoiceSettings.Stability
		}
		if opts.VoiceSettings.SimilarityBoost != nil {
			voiceSettings["similarity_boost"] = *opts.VoiceSettings.SimilarityBoost
		}
		if opts.VoiceSettings.Style != nil {
			voiceSettings["style"] = *opts.VoiceSettings.Style
		}
		if opts.VoiceSettings.UseSpeakerBoost != nil {
			voiceSettings["use_speaker_boost"] = *opts.VoiceSettings.UseSpeakerBoost
		}
	}
	if opts.LanguageCode != "" {
		if _, ok := body["language_code"]; !ok {
			body["language_code"] = opts.LanguageCode
		}
	}
	if len(opts.PronunciationDictionaryLocators) > 0 {
		locators := make([]map[string]interface{}, 0, len(opts.PronunciationDictionaryLocators))
		for _, locator := range opts.PronunciationDictionaryLocators {
			item := map[string]interface{}{
				"pronunciation_dictionary_id": locator.PronunciationDictionaryID,
			}
			if locator.VersionID != "" {
				item["version_id"] = locator.VersionID
			}
			locators = append(locators, item)
		}
		body["pronunciation_dictionary_locators"] = locators
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	if opts.PreviousText != "" {
		body["previous_text"] = opts.PreviousText
	}
	if opts.NextText != "" {
		body["next_text"] = opts.NextText
	}
	if len(opts.PreviousRequestIDs) > 0 {
		body["previous_request_ids"] = opts.PreviousRequestIDs
	}
	if len(opts.NextRequestIDs) > 0 {
		body["next_request_ids"] = opts.NextRequestIDs
	}
	if opts.ApplyTextNormalization != "" {
		body["apply_text_normalization"] = opts.ApplyTextNormalization
	}
	if opts.ApplyLanguageTextNormalization != nil {
		body["apply_language_text_normalization"] = *opts.ApplyLanguageTextNormalization
	}
	if opts.EnableLogging != nil {
		queryParams["enable_logging"] = fmt.Sprintf("%t", *opts.EnableLogging)
	}
}

func extractSpeechModelOptions(providerOptions map[string]interface{}) (SpeechModelOptions, bool) {
	if providerOptions == nil {
		return SpeechModelOptions{}, false
	}
	raw, ok := providerOptions["elevenlabs"]
	if !ok {
		return SpeechModelOptions{}, false
	}
	switch value := raw.(type) {
	case SpeechModelOptions:
		return value, true
	case *SpeechModelOptions:
		if value == nil {
			return SpeechModelOptions{}, true
		}
		return *value, true
	case map[string]interface{}:
		return parseSpeechModelOptionsMap(value), true
	default:
		return SpeechModelOptions{}, true
	}
}

func parseSpeechModelOptionsMap(raw map[string]interface{}) SpeechModelOptions {
	var opts SpeechModelOptions
	if v, ok := raw["languageCode"].(string); ok {
		opts.LanguageCode = v
	}
	if v, ok := raw["voiceSettings"].(map[string]interface{}); ok {
		voiceSettings := SpeechVoiceSettings{}
		if n, ok := numberAsFloat(v["stability"]); ok {
			voiceSettings.Stability = &n
		}
		if n, ok := numberAsFloat(v["similarityBoost"]); ok {
			voiceSettings.SimilarityBoost = &n
		}
		if n, ok := numberAsFloat(v["style"]); ok {
			voiceSettings.Style = &n
		}
		if b, ok := v["useSpeakerBoost"].(bool); ok {
			voiceSettings.UseSpeakerBoost = &b
		}
		opts.VoiceSettings = &voiceSettings
	}
	if v, ok := raw["pronunciationDictionaryLocators"].([]PronunciationDictionaryLocator); ok {
		opts.PronunciationDictionaryLocators = v
	} else if v, ok := raw["pronunciationDictionaryLocators"].([]interface{}); ok {
		opts.PronunciationDictionaryLocators = parsePronunciationLocators(v)
	}
	if n, ok := numberAsFloat(raw["seed"]); ok {
		opts.Seed = &n
	}
	if v, ok := raw["previousText"].(string); ok {
		opts.PreviousText = v
	}
	if v, ok := raw["nextText"].(string); ok {
		opts.NextText = v
	}
	if v, ok := stringSlice(raw["previousRequestIds"]); ok {
		opts.PreviousRequestIDs = v
	}
	if v, ok := stringSlice(raw["nextRequestIds"]); ok {
		opts.NextRequestIDs = v
	}
	if v, ok := raw["applyTextNormalization"].(string); ok {
		opts.ApplyTextNormalization = v
	}
	if v, ok := raw["applyLanguageTextNormalization"].(bool); ok {
		opts.ApplyLanguageTextNormalization = &v
	}
	if v, ok := raw["enableLogging"].(bool); ok {
		opts.EnableLogging = &v
	}
	return opts
}

func parsePronunciationLocators(raw []interface{}) []PronunciationDictionaryLocator {
	locators := make([]PronunciationDictionaryLocator, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		locator := PronunciationDictionaryLocator{}
		if v, ok := m["pronunciationDictionaryId"].(string); ok {
			locator.PronunciationDictionaryID = v
		}
		if v, ok := m["versionId"].(string); ok {
			locator.VersionID = v
		}
		locators = append(locators, locator)
	}
	return locators
}

func stringSlice(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func numberAsFloat(value interface{}) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
