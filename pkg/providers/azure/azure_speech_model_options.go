package azure

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AzureSpeechModelSpeechOptions is providerOptions.azure for the Azure
// Speech SSML speech backend (MAI-Voice-2 and variants), mirroring TS
// AzureSpeechModelSpeechOptions.
type AzureSpeechModelSpeechOptions struct {
	// Style is applied with mstts:express-as, e.g. "excited".
	Style string
	// StyleDegree is the intensity of Style, from 0.01 to 2.
	StyleDegree *float64
}

// AzureSpeechModelOptions is the fully parsed providerOptions.azure for
// azure.Speech, mirroring TS AzureSpeechModelOptions.
type AzureSpeechModelOptions struct {
	API AzureTranscriptionAPI // reuses "openai" | "speech"
	AzureSpeechModelSpeechOptions
}

// azureVoiceModelsByLowercaseID maps a lowercase model ID to its Azure
// voice-name suffix, mirroring TS getMAIVoiceModel / maiVoiceModels.
var azureVoiceModelsByLowercaseID = map[string]string{
	"mai-voice-2":         "MAI-Voice-2",
	"mai-voice-2-flash":   "MAI-Voice-2-Flash",
	"mai-voice-2.1":       "MAI-Voice-2.1",
	"mai-voice-2.1-flash": "MAI-Voice-2.1-Flash",
}

// getMAIVoiceModel returns the Azure voice-name suffix for modelID,
// mirroring TS getMAIVoiceModel. ok is false for any other model ID.
func getMAIVoiceModel(modelID string) (string, bool) {
	name, ok := azureVoiceModelsByLowercaseID[strings.ToLower(modelID)]
	return name, ok
}

// parseAzureSpeechProviderOptions parses and strictly validates
// providerOptions["azure"] for azure.Speech, mirroring TS
// azureSpeechModelOptions: unknown keys fail with InvalidArgumentError.
func parseAzureSpeechProviderOptions(providerOptions map[string]interface{}) (AzureSpeechModelOptions, error) {
	var result AzureSpeechModelOptions
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

	allowed := map[string]bool{"api": true, "style": true, "styleDegree": true}
	for key := range m {
		if !allowed[key] {
			return result, invalidAzureOption("providerOptions.azure."+key, "unrecognized key")
		}
	}

	if v, ok := m["api"]; ok {
		s, ok := v.(string)
		if !ok || (s != "openai" && s != "speech") {
			return result, invalidAzureOption("providerOptions.azure.api", `must be "openai" or "speech"`)
		}
		result.API = AzureTranscriptionAPI(s)
	}
	if v, ok := m["style"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return result, invalidAzureOption("providerOptions.azure.style", "must be a non-empty string")
		}
		result.Style = s
	}
	if v, ok := m["styleDegree"]; ok {
		f, ok := toFloat64Option(v)
		if !ok || f < 0.01 || f > 2 {
			return result, invalidAzureOption("providerOptions.azure.styleDegree", "must be a number between 0.01 and 2")
		}
		result.StyleDegree = &f
	}

	return result, nil
}

func toFloat64Option(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// azureSpeechOnlyWarnings returns an "unsupported" warning for each
// speech-only option that was explicitly set, mirroring TS
// speechOnlyWarnings (used when a call with Speech-only options is routed to
// the OpenAI backend instead).
func azureSpeechOnlyWarnings(opts AzureSpeechModelSpeechOptions) []types.Warning {
	var warnings []types.Warning
	if opts.Style != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.azure.style",
			Details: "This option requires the Azure Speech API.",
		})
	}
	if opts.StyleDegree != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.azure.styleDegree",
			Details: "This option requires the Azure Speech API.",
		})
	}
	return warnings
}
