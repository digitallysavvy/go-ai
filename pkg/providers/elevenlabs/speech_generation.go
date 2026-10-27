package elevenlabs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func intRef(v int) *int { return &v }

func isMusicModelID(modelID string) bool { return strings.HasPrefix(modelID, "music_") }

func isSoundEffectsModelID(modelID string) bool {
	return strings.HasPrefix(modelID, "eleven_text_to_sound")
}

// Music and sound effects model IDs (TS ElevenLabsSpeechModelId).
const (
	SpeechModelMusicV25 = "music_v2_5"
	SpeechModelMusicV2  = "music_v2"
	SpeechModelMusicV1  = "music_v1"

	SpeechModelTextToSoundV2 = "eleven_text_to_sound_v2"
	SpeechModelTextToSoundV3 = "eleven_text_to_sound_v3"
)

// Options that only the text-to-speech and dialogue endpoints read.
var textToSpeechOnlyOptions = []string{
	"applyLanguageTextNormalization", "applyTextNormalization", "dialogue", "inputs", "languageCode",
	"nextRequestIds", "nextText", "previousRequestIds", "previousText", "pronunciationDictionaryLocators",
	"streaming", "voiceSettings",
}

var (
	musicOnlyOptions        = []string{"compositionPlan", "forceInstrumental", "musicLengthMs"}
	soundEffectsOnlyOptions = []string{"durationSeconds", "loop", "promptInfluence"}
)

// isSet reports whether the named option is set (TS `option != null`).
func (o SpeechModelOptions) isSet(name string) bool {
	switch name {
	case "applyLanguageTextNormalization":
		return o.ApplyLanguageTextNormalization != nil
	case "applyTextNormalization":
		return o.ApplyTextNormalization != ""
	case "dialogue":
		return o.Dialogue != nil
	case "inputs":
		return o.Inputs != nil
	case "languageCode":
		return o.LanguageCode != ""
	case "nextRequestIds":
		return o.NextRequestIDs != nil
	case "nextText":
		return o.NextText != ""
	case "previousRequestIds":
		return o.PreviousRequestIDs != nil
	case "previousText":
		return o.PreviousText != ""
	case "pronunciationDictionaryLocators":
		return o.PronunciationDictionaryLocators != nil
	case "streaming":
		return o.Streaming != nil
	case "voiceSettings":
		return o.VoiceSettings != nil
	case "seed":
		return o.Seed != nil
	case "compositionPlan":
		return o.CompositionPlan != nil
	case "forceInstrumental":
		return o.ForceInstrumental != nil
	case "musicLengthMs":
		return o.MusicLengthMs != nil
	case "durationSeconds":
		return o.DurationSeconds != nil
	case "loop":
		return o.Loop != nil
	case "promptInfluence":
		return o.PromptInfluence != nil
	}
	return false
}

// speechCommonWarnings are the warnings every text-to-speech and dialogue
// request starts with: streaming-only options, then music and sound effects
// options, none of which the endpoint reads.
func speechCommonWarnings(o SpeechModelOptions) []types.Warning {
	var warnings []types.Warning
	if o.Streaming != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.elevenlabs.streaming",
			Details: "Streaming options apply only to streaming speech (ExperimentalStreamSpeech) and were ignored.",
		})
	}
	if o.Dialogue != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.elevenlabs.dialogue",
			Details: "Dialogue options apply only to streaming speech (ExperimentalStreamSpeech) and were ignored.",
		})
	}
	for _, option := range append(append([]string{}, musicOnlyOptions...), soundEffectsOnlyOptions...) {
		if o.isSet(option) {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.elevenlabs." + option,
				Details: fmt.Sprintf("ElevenLabs speech models do not support %s; it was ignored.", option),
			})
		}
	}
	return warnings
}

func locatorBody(locators []PronunciationDictionaryLocator) []interface{} {
	out := make([]interface{}, 0, len(locators))
	for _, locator := range locators {
		item := map[string]interface{}{"pronunciation_dictionary_id": locator.PronunciationDictionaryID}
		if locator.VersionID != "" {
			item["version_id"] = locator.VersionID
		}
		out = append(out, item)
	}
	return out
}

var dialogueBodyOrder = map[string][]string{
	"": {"inputs", "model_id", "language_code", "settings", "previous_text", "future_text",
		"pronunciation_dictionary_locators", "seed", "apply_text_normalization", "previous_request_ids", "next_request_ids"},
	"inputs":                            {"text", "voice_id"},
	"settings":                          {"stability", "similarity"},
	"pronunciation_dictionary_locators": {"pronunciation_dictionary_id", "version_id"},
}

var musicBodyOrder = map[string][]string{
	"": {"composition_plan", "prompt", "model_id", "music_length_ms", "force_instrumental", "seed"},
}

var soundEffectsBodyOrder = map[string][]string{
	"": {"text", "model_id", "duration_seconds", "prompt_influence", "loop"},
}

// buildDialogueRequest is the multi-voice dialogue request (/v1/text-to-dialogue)
// from providerOptions.elevenlabs.inputs, which replaces text and voice.
func (m *SpeechModel) buildDialogueRequest(opts *provider.SpeechGenerateOptions, o SpeechModelOptions) (*speechRequest, error) {
	if !isDialogueModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "dialogue with " + m.modelID,
			Message:       fmt.Sprintf("ElevenLabs dialogue requires an eleven_v3 or eleven_v4 model; %s is not one.", m.modelID),
		}
	}
	warnings := speechCommonWarnings(o)
	if opts.Text != "" {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "ElevenLabs dialogue uses providerOptions.elevenlabs.inputs; the text prompt was ignored.",
		})
	}
	var ignored []string
	if opts.Instructions != "" {
		ignored = append(ignored, "instructions")
	}
	if opts.Speed != nil {
		ignored = append(ignored, "speed")
	}
	if opts.Voice != "" {
		ignored = append(ignored, "voice")
	}
	if o.ApplyLanguageTextNormalization != nil {
		ignored = append(ignored, "providerOptions.elevenlabs.applyLanguageTextNormalization")
	}
	if o.VoiceSettings != nil && o.VoiceSettings.Style != nil {
		ignored = append(ignored, "providerOptions.elevenlabs.voiceSettings.style")
	}
	if o.VoiceSettings != nil && o.VoiceSettings.UseSpeakerBoost != nil {
		ignored = append(ignored, "providerOptions.elevenlabs.voiceSettings.useSpeakerBoost")
	}
	for _, feature := range ignored {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: feature,
			Details: fmt.Sprintf("ElevenLabs dialogue does not support %s; it was ignored.", feature),
		})
	}

	query := map[string]string{"output_format": speechOutputFormat(opts.OutputFormat)}
	if o.EnableLogging != nil {
		query["enable_logging"] = strconv.FormatBool(*o.EnableLogging)
	}

	inputs := make([]interface{}, 0, len(o.Inputs))
	for _, input := range o.Inputs {
		inputs = append(inputs, map[string]interface{}{"text": input.Text, "voice_id": input.VoiceID})
	}
	body := map[string]interface{}{"inputs": inputs, "model_id": m.modelID}

	settings := map[string]interface{}{}
	if o.VoiceSettings != nil {
		if o.VoiceSettings.Stability != nil {
			settings["stability"] = *o.VoiceSettings.Stability
		}
		// Text-to-Dialogue names this setting `similarity`.
		if o.VoiceSettings.SimilarityBoost != nil {
			settings["similarity"] = *o.VoiceSettings.SimilarityBoost
		}
	}
	language := opts.Language
	if language == "" {
		language = o.LanguageCode
	}
	if language != "" {
		body["language_code"] = language
	}
	if len(settings) > 0 {
		body["settings"] = settings
	}
	if o.PreviousText != "" {
		body["previous_text"] = o.PreviousText
	}
	if o.NextText != "" {
		body["future_text"] = o.NextText
	}
	if o.PronunciationDictionaryLocators != nil {
		body["pronunciation_dictionary_locators"] = locatorBody(o.PronunciationDictionaryLocators)
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.ApplyTextNormalization != "" {
		body["apply_text_normalization"] = o.ApplyTextNormalization
	}
	if o.PreviousRequestIDs != nil {
		body["previous_request_ids"] = o.PreviousRequestIDs
	}
	if o.NextRequestIDs != nil {
		body["next_request_ids"] = o.NextRequestIDs
	}

	return &speechRequest{
		path: "/v1/text-to-dialogue", body: body, bodyOrder: dialogueBodyOrder, query: query, warnings: warnings,
	}, nil
}

func speechOutputFormat(outputFormat string) string {
	if outputFormat == "" {
		return defaultSpeechOutputFormat
	}
	return mapElevenLabsOutputFormat(outputFormat)
}

// buildAudioGenerationRequest prepares a music (/v1/music) or sound effects
// (/v1/sound-generation) request.
func (m *SpeechModel) buildAudioGenerationRequest(opts *provider.SpeechGenerateOptions) (*speechRequest, error) {
	isMusic := isMusicModelID(m.modelID)
	label := "sound effects"
	if isMusic {
		label = "music"
	}
	o, _ := extractSpeechModelOptions(opts.ProviderOptions)
	warnings := []types.Warning{}

	ignoredCall := []struct {
		feature string
		set     bool
	}{
		{"instructions", opts.Instructions != ""},
		{"language", opts.Language != ""},
		{"speed", opts.Speed != nil},
		{"voice", opts.Voice != ""},
	}
	for _, c := range ignoredCall {
		if c.set {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: c.feature,
				Details: fmt.Sprintf("ElevenLabs %s models do not support %s; it was ignored.", label, c.feature),
			})
		}
	}
	ignoredOptions := append([]string{}, textToSpeechOnlyOptions...)
	if isMusic {
		ignoredOptions = append(ignoredOptions, soundEffectsOnlyOptions...)
	} else {
		ignoredOptions = append(ignoredOptions, musicOnlyOptions...)
		ignoredOptions = append(ignoredOptions, "seed")
	}
	for _, option := range ignoredOptions {
		if o.isSet(option) {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.elevenlabs." + option,
				Details: fmt.Sprintf("ElevenLabs %s models do not support %s; it was ignored.", label, option),
			})
		}
	}

	query := map[string]string{"output_format": speechOutputFormat(opts.OutputFormat)}
	if o.EnableLogging != nil {
		query["enable_logging"] = strconv.FormatBool(*o.EnableLogging)
	}

	req := &speechRequest{query: query}
	if isMusic {
		req.path = "/v1/music"
		req.bodyOrder = musicBodyOrder
		body := map[string]interface{}{"model_id": m.modelID}
		if o.CompositionPlan != nil {
			// The API rejects prompt-only options alongside a composition plan.
			if o.MusicLengthMs != nil {
				return nil, compositionPlanConflict("musicLengthMs")
			}
			if o.ForceInstrumental != nil {
				return nil, compositionPlanConflict("forceInstrumental")
			}
			if opts.Text != "" {
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: "ElevenLabs music uses providerOptions.elevenlabs.compositionPlan; the text prompt was ignored.",
				})
			}
			if d, ok := compositionPlanDurationMs(o.CompositionPlan); ok {
				req.requestedDurationMs = &d
			}
			body["composition_plan"] = o.CompositionPlan
		} else {
			body["prompt"] = opts.Text
			if o.MusicLengthMs != nil {
				d := float64(*o.MusicLengthMs)
				req.requestedDurationMs = &d
			}
		}
		if o.MusicLengthMs != nil {
			body["music_length_ms"] = *o.MusicLengthMs
		}
		if o.ForceInstrumental != nil {
			body["force_instrumental"] = *o.ForceInstrumental
		}
		if o.Seed != nil {
			body["seed"] = *o.Seed
		}
		req.body = body
	} else {
		req.path = "/v1/sound-generation"
		req.bodyOrder = soundEffectsBodyOrder
		body := map[string]interface{}{"text": opts.Text, "model_id": m.modelID}
		if o.DurationSeconds != nil {
			body["duration_seconds"] = *o.DurationSeconds
			d := *o.DurationSeconds * 1000
			req.requestedDurationMs = &d
		}
		if o.PromptInfluence != nil {
			body["prompt_influence"] = *o.PromptInfluence
		}
		if o.Loop != nil {
			body["loop"] = *o.Loop
		}
		req.body = body
	}
	req.warnings = warnings
	return req, nil
}

func compositionPlanConflict(option string) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions.elevenlabs." + option,
		Message: fmt.Sprintf("ElevenLabs music %s cannot be combined with compositionPlan.", option),
	}
}
