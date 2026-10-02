package azure

import (
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AzureTranscriptionAPI selects which backend handles an azure.Transcription
// call, mirroring TS's `api: 'openai' | 'speech' | 'mai'` provider option.
type AzureTranscriptionAPI string

const (
	AzureTranscriptionAPIOpenAI AzureTranscriptionAPI = "openai"
	AzureTranscriptionAPISpeech AzureTranscriptionAPI = "speech"
	AzureTranscriptionAPIMai    AzureTranscriptionAPI = "mai"
)

// AzureDiarizationOptions mirrors TS's `diarization: { enabled }` shape.
type AzureDiarizationOptions struct {
	Enabled bool
}

// AzurePhraseListOptions mirrors TS's `phraseList: { phrases }` shape.
type AzurePhraseListOptions struct {
	Phrases []string
}

// AzureSpeechTranscriptionOptions is providerOptions.azure for the Azure
// Speech file-transcription backend (MAI-Transcribe-1.5 / MAI-Transcribe-2),
// mirroring TS AzureTranscriptionModelSpeechOptions.
type AzureSpeechTranscriptionOptions struct {
	// Timestamps selects "word", "segment", or "none" granularity.
	Timestamps string
	// TranscribeStyle is "verbatim" (default) or "clean".
	TranscribeStyle string
	// Locales forces a single language, e.g. []string{"en"}.
	Locales []string
	// Diarization enables speaker diarization.
	Diarization *AzureDiarizationOptions
	// PhraseList biases recognition toward specific phrases.
	PhraseList *AzurePhraseListOptions
}

// AzureTranscriptionModelOptions is the fully parsed providerOptions.azure
// for azure.Transcription, mirroring TS AzureTranscriptionModelOptions.
type AzureTranscriptionModelOptions struct {
	API AzureTranscriptionAPI
	AzureSpeechTranscriptionOptions
	// Language is a MAI-streaming-only language hint.
	Language string
}

// azureTranscribeModelsByLowercaseID maps a lowercase model ID to its Azure
// Speech model name and whether it supports the `timestamps` option, mirroring
// TS getMAITranscribeModel / maiTranscribeModels. MAI-Transcribe-1.5 only
// accepts Azure's default timestamps ("none").
var azureTranscribeModelsByLowercaseID = map[string]struct {
	name               string
	supportsTimestamps bool
}{
	"mai-transcribe-2":   {name: "MAI-Transcribe-2", supportsTimestamps: true},
	"mai-transcribe-1.5": {name: "MAI-Transcribe-1.5", supportsTimestamps: false},
}

// azureMAITranscribeModel is the Azure Speech model name and option-support
// flags for a recognized MAI-Transcribe model ID (empty name = not MAI).
type azureMAITranscribeModel struct {
	Name               string
	SupportsTimestamps bool
}

// getMAITranscribeModel reports the Azure Speech model name for modelID,
// mirroring TS getMAITranscribeModel. ok is false for any other model ID.
func getMAITranscribeModel(modelID string) (azureMAITranscribeModel, bool) {
	m, ok := azureTranscribeModelsByLowercaseID[strings.ToLower(modelID)]
	if !ok {
		return azureMAITranscribeModel{}, false
	}
	return azureMAITranscribeModel{Name: m.name, SupportsTimestamps: m.supportsTimestamps}, true
}

// isMAITranscribeStreaming reports whether modelID is
// MAI-Transcribe-2-Streaming, mirroring TS isMAITranscribeStreaming.
func isMAITranscribeStreaming(modelID string) bool {
	return strings.EqualFold(modelID, "mai-transcribe-2-streaming")
}

// parseAzureTranscriptionProviderOptions parses and strictly validates
// providerOptions["azure"] for azure.Transcription, mirroring TS
// azureTranscriptionModelOptions (a strict Zod object): unknown keys, or a
// malformed diarization/phraseList/locales shape, fail with
// InvalidArgumentError instead of being silently dropped.
func parseAzureTranscriptionProviderOptions(providerOptions map[string]interface{}) (AzureTranscriptionModelOptions, error) {
	var result AzureTranscriptionModelOptions
	if providerOptions == nil {
		return result, nil
	}
	raw, ok := providerOptions["azure"]
	if !ok || raw == nil {
		return result, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return result, invalidAzureOption("providerOptions.azure", "must be an object")
	}

	allowed := map[string]bool{
		"api": true, "timestamps": true, "transcribeStyle": true,
		"locales": true, "diarization": true, "phraseList": true, "language": true,
	}
	for key := range m {
		if !allowed[key] {
			return result, invalidAzureOption("providerOptions.azure."+key, "unrecognized key")
		}
	}

	if v, ok := m["api"]; ok {
		s, ok := v.(string)
		if !ok || (s != "openai" && s != "speech" && s != "mai") {
			return result, invalidAzureOption("providerOptions.azure.api", `must be "openai", "speech", or "mai"`)
		}
		result.API = AzureTranscriptionAPI(s)
	}
	if v, ok := m["timestamps"]; ok {
		s, ok := v.(string)
		if !ok || (s != "word" && s != "segment" && s != "none") {
			return result, invalidAzureOption("providerOptions.azure.timestamps", `must be "word", "segment", or "none"`)
		}
		result.Timestamps = s
	}
	if v, ok := m["transcribeStyle"]; ok {
		s, ok := v.(string)
		if !ok || (s != "verbatim" && s != "clean") {
			return result, invalidAzureOption("providerOptions.azure.transcribeStyle", `must be "verbatim" or "clean"`)
		}
		result.TranscribeStyle = s
	}
	if v, ok := m["locales"]; ok {
		arr, ok := v.([]interface{})
		if !ok || len(arr) != 1 {
			return result, invalidAzureOption("providerOptions.azure.locales", "must be an array with exactly one locale")
		}
		locale, ok := arr[0].(string)
		if !ok {
			return result, invalidAzureOption("providerOptions.azure.locales", "must contain a string")
		}
		result.Locales = []string{locale}
	}
	if v, ok := m["diarization"]; ok {
		dm, ok := v.(map[string]interface{})
		if !ok {
			return result, invalidAzureOption("providerOptions.azure.diarization", "must be an object")
		}
		for key := range dm {
			if key != "enabled" {
				return result, invalidAzureOption("providerOptions.azure.diarization."+key, "unrecognized key")
			}
		}
		enabled, ok := dm["enabled"].(bool)
		if !ok {
			return result, invalidAzureOption("providerOptions.azure.diarization.enabled", "must be a boolean")
		}
		result.Diarization = &AzureDiarizationOptions{Enabled: enabled}
	}
	if v, ok := m["phraseList"]; ok {
		pm, ok := v.(map[string]interface{})
		if !ok {
			return result, invalidAzureOption("providerOptions.azure.phraseList", "must be an object")
		}
		for key := range pm {
			if key != "phrases" {
				return result, invalidAzureOption("providerOptions.azure.phraseList."+key, "unrecognized key")
			}
		}
		arr, ok := pm["phrases"].([]interface{})
		if !ok {
			return result, invalidAzureOption("providerOptions.azure.phraseList.phrases", "must be an array of strings")
		}
		phrases := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return result, invalidAzureOption("providerOptions.azure.phraseList.phrases", "must be an array of strings")
			}
			phrases = append(phrases, s)
		}
		result.PhraseList = &AzurePhraseListOptions{Phrases: phrases}
	}
	if v, ok := m["language"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return result, invalidAzureOption("providerOptions.azure.language", "must be a non-empty string")
		}
		result.Language = s
	}

	return result, nil
}

func invalidAzureOption(field, message string) error {
	return &providererrors.InvalidArgumentError{Field: field, Message: message}
}

// azureTranscriptionSpeechOnlyWarnings returns an "unsupported" warning for
// each speech-only option that was explicitly set, mirroring TS
// speechOnlyWarnings (used when a call with Speech-only options is routed to
// the OpenAI or MAI backend instead).
func azureTranscriptionSpeechOnlyWarnings(opts AzureSpeechTranscriptionOptions) []types.Warning {
	var warnings []types.Warning
	add := func(key string) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.azure." + key,
			Details: "This option requires the Azure Speech API.",
		})
	}
	if opts.Timestamps != "" {
		add("timestamps")
	}
	if opts.TranscribeStyle != "" {
		add("transcribeStyle")
	}
	if opts.Locales != nil {
		add("locales")
	}
	if opts.Diarization != nil {
		add("diarization")
	}
	if opts.PhraseList != nil {
		add("phraseList")
	}
	return warnings
}

// azureTranscriptionMaiOnlyWarnings returns an "unsupported" warning when
// language was set but the call was not routed to MAI streaming
// transcription, mirroring TS maiOnlyWarnings.
func azureTranscriptionMaiOnlyWarnings(language string) []types.Warning {
	if language == "" {
		return nil
	}
	return []types.Warning{{
		Type:    "unsupported",
		Feature: "providerOptions.azure.language",
		Details: "This option requires MAI streaming transcription.",
	}}
}
