package elevenlabs

import (
	"context"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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

	// Streaming holds options for ai.ExperimentalStreamSpeech.
	Streaming *StreamingSpeechOptions

	// Dialogue streams the speech as Text-to-Dialogue (eleven_v3 and
	// eleven_v4 models, ai.ExperimentalStreamSpeech only).
	Dialogue *DialogueOptions

	// Inputs generates a multi-voice dialogue with ai.GenerateSpeech (eleven_v3
	// and eleven_v4 models). It replaces the text and the voice.
	Inputs []DialogueInput

	// Music options (music_ models).

	// MusicLengthMs is the length of the track, 3000 to 600000. Leave unset to
	// let the model decide.
	MusicLengthMs *int
	// ForceInstrumental forces an instrumental track.
	ForceInstrumental *bool
	// CompositionPlan is a native (snake_case) composition plan sent in place
	// of the prompt. It cannot be combined with MusicLengthMs or
	// ForceInstrumental.
	CompositionPlan map[string]interface{}

	// Sound effects options (eleven_text_to_sound models).

	// DurationSeconds is the length of the effect, 0.5 to 30.
	DurationSeconds *float64
	// PromptInfluence is how closely to follow the prompt, 0 to 1.
	PromptInfluence *float64
	// Loop makes the effect loop smoothly.
	Loop *bool
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
	return "elevenlabs.speech"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis. Music (music_) and sound effects
// (eleven_text_to_sound) models take a text prompt and return audio, so they
// share this interface; providerOptions.elevenlabs.inputs generates a
// multi-voice dialogue with an eleven_v3 or eleven_v4 model.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if err := validateSpeechProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	currentDate := time.Now()
	req, err := m.buildRequest(opts)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    req.path,
		Body:    req.body,
		Query:   req.query,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err // TS postJsonToApi lets a transport failure propagate unchanged
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// TS elevenlabsFailedResponseHandler: {"error":{"message","code"}} or
		// the HTTP status text.
		return nil, handleTranscriptionError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
	}
	requestBytes := []byte(providerutils.StringifyOrdered(req.body, req.bodyOrder))

	metadata := map[string]interface{}{}
	if format, ok := req.query["output_format"]; ok {
		metadata["outputFormat"] = format
	}
	if req.requestedDurationMs != nil {
		metadata["requestedDurationMs"] = *req.requestedDurationMs
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
		Warnings: req.warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
		},
		ProviderMetadata: map[string]interface{}{"elevenlabs": metadata},
	}, nil
}

// speechRequest is a prepared HTTP speech request.
type speechRequest struct {
	path      string
	body      map[string]interface{}
	bodyOrder map[string][]string
	query     map[string]string
	warnings  []types.Warning

	// requestedDurationMs is the length asked for (music and sound effects).
	requestedDurationMs *float64
}

// buildRequest prepares the request for the model kind: music, sound effects,
// dialogue (providerOptions.elevenlabs.inputs), or text-to-speech.
func (m *SpeechModel) buildRequest(opts *provider.SpeechGenerateOptions) (*speechRequest, error) {
	if isMusicModelID(m.modelID) || isSoundEffectsModelID(m.modelID) {
		return m.buildAudioGenerationRequest(opts)
	}
	if elOpts, ok := extractSpeechModelOptions(opts.ProviderOptions); ok && elOpts.Inputs != nil {
		return m.buildDialogueRequest(opts, elOpts)
	}
	body, query, warnings, voice := m.buildRequestArgs(opts)
	return &speechRequest{
		path:      fmt.Sprintf("/v1/text-to-speech/%s", voice),
		body:      body,
		bodyOrder: elevenlabsSpeechBodyOrder,
		query:     query,
		warnings:  warnings,
	}, nil
}

// elevenlabsSpeechBodyOrder is the key order of the TS requestBody
// (elevenlabs-speech-model.ts); voice_settings is assigned last.
var elevenlabsSpeechBodyOrder = map[string][]string{
	"": {"text", "model_id", "language_code", "pronunciation_dictionary_locators", "seed", "previous_text",
		"next_text", "previous_request_ids", "next_request_ids", "apply_text_normalization",
		"apply_language_text_normalization", "voice_settings"},
	"pronunciation_dictionary_locators": {"pronunciation_dictionary_id", "version_id"},
	"voice_settings":                    {"speed", "stability", "similarity_boost", "style", "use_speaker_boost"},
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
	elevenLabsOptions, hasOptions := extractSpeechModelOptions(opts.ProviderOptions)
	if hasOptions {
		warnings = append(warnings, speechCommonWarnings(elevenLabsOptions)...)
	}
	if opts.Language != "" {
		body["language_code"] = opts.Language
	}
	if opts.Speed != nil {
		voiceSettings["speed"] = *opts.Speed
	}
	if hasOptions {
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
	// JS truthiness: an empty array is truthy and is sent as [].
	if opts.PronunciationDictionaryLocators != nil {
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
	if opts.PreviousRequestIDs != nil {
		body["previous_request_ids"] = opts.PreviousRequestIDs
	}
	if opts.NextRequestIDs != nil {
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
	if v, ok := raw["streaming"].(map[string]interface{}); ok {
		opts.Streaming = parseStreamingSpeechOptions(v)
	}
	if v, ok := raw["dialogue"].(map[string]interface{}); ok {
		opts.Dialogue = parseDialogueOptions(v)
	}
	if v, present := raw["inputs"]; present {
		opts.Inputs = parseDialogueInputs(v)
	}
	if n, ok := numberAsFloat(raw["musicLengthMs"]); ok {
		i := int(n)
		opts.MusicLengthMs = &i
	}
	if v, ok := raw["forceInstrumental"].(bool); ok {
		opts.ForceInstrumental = &v
	}
	if v, ok := raw["compositionPlan"].(map[string]interface{}); ok {
		opts.CompositionPlan = v
	}
	if n, ok := numberAsFloat(raw["durationSeconds"]); ok {
		opts.DurationSeconds = &n
	}
	if n, ok := numberAsFloat(raw["promptInfluence"]); ok {
		opts.PromptInfluence = &n
	}
	if v, ok := raw["loop"].(bool); ok {
		opts.Loop = &v
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

// elevenLabsSpeechOptionsSchema is elevenLabsSpeechModelOptionsSchema
// (elevenlabs-speech-model-options.ts): every member is .optional(), so an
// explicit null fails.
var elevenLabsSpeechOptionsSchema = func() providerutils.OptionSchema {
	unit := providerutils.OptRange(0, 1)
	three := func(item providerutils.OptionCheck) providerutils.OptionCheck {
		return providerutils.OptArray(item, 0, 3)
	}
	return providerutils.OptionSchema{
		"languageCode": providerutils.OptString,
		"voiceSettings": providerutils.OptObject(providerutils.OptionSchema{
			"stability": unit, "similarityBoost": unit, "style": unit,
			"useSpeakerBoost": providerutils.OptBool,
		}),
		"pronunciationDictionaryLocators": func(path string, v interface{}) error {
			locator := providerutils.OptObjectWith(providerutils.OptionSchema{
				"pronunciationDictionaryId": providerutils.OptString, "versionId": providerutils.OptString,
			}, "pronunciationDictionaryId")
			// Typed locators are valid by construction; only their count is checked.
			return three(func(p string, item interface{}) error {
				if _, typed := item.(PronunciationDictionaryLocator); typed {
					return nil
				}
				return locator(p, item)
			})(path, v)
		},
		"seed":                           providerutils.OptRange(0, 4294967295),
		"previousText":                   providerutils.OptString,
		"nextText":                       providerutils.OptString,
		"previousRequestIds":             three(providerutils.OptString),
		"nextRequestIds":                 three(providerutils.OptString),
		"applyTextNormalization":         providerutils.OptEnum("auto", "on", "off"),
		"applyLanguageTextNormalization": providerutils.OptBool,
		"enableLogging":                  providerutils.OptBool,
		"streaming":                      streamingSpeechOptionsCheck(),
		"dialogue":                       dialogueOptionsCheck(),
		"inputs":                         dialogueInputsCheck(),
		"musicLengthMs":                  providerutils.OptInt(intRef(3000), intRef(600000)),
		"forceInstrumental":              providerutils.OptBool,
		"compositionPlan":                compositionPlanCheck(),
		"durationSeconds":                providerutils.OptRange(0.5, 30),
		"promptInfluence":                providerutils.OptRange(0, 1),
		"loop":                           providerutils.OptBool,
	}
}()

// validateSpeechProviderOptions is TS parseProviderOptions with
// elevenLabsSpeechModelOptionsSchema; typed option structs are valid by
// construction.
func validateSpeechProviderOptions(providerOptions map[string]interface{}) error {
	if _, ok := providerOptions["elevenlabs"].(map[string]interface{}); !ok {
		return nil
	}
	_, err := providerutils.ParseProviderOptions("elevenlabs", providerOptions, elevenLabsSpeechOptionsSchema)
	return err
}
