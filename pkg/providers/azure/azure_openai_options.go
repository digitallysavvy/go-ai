package azure

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// OpenAI API option keys that Azure OpenAI deployments also accept under
// providerOptions.azure (TS 6400cf8b88: openAITranscriptionModelOptionsShape,
// openaiSpeechModelOptionsShape, openaiImageModelGenerationOptionsShape and
// openaiImageModelEditOptionsShape from @ai-sdk/openai/internal), in schema
// order.
var (
	azureOpenAITranscriptionOptionKeys = []string{"include", "language", "languages", "keywords", "prompt", "temperature", "timestampGranularities", "responseFormat", "chunkingStrategy", "streaming"}
	azureOpenAISpeechOptionKeys        = []string{"instructions", "speed"}
	azureOpenAIImageGenerationKeys     = []string{"quality", "background", "outputFormat", "complexity", "outputCompression", "user", "safetyIdentifier", "style", "frameCount", "frameDurationMs", "moderation"}
	azureOpenAIImageEditKeys           = []string{"quality", "background", "outputFormat", "complexity", "outputCompression", "user", "safetyIdentifier", "inputFidelity", "markup", "moderation"}
)

// azurePickedOptions are the raw providerOptions.azure values of one OpenAI
// option shape, in shape order (TS pickAzureOptions: raw values, so no schema
// defaults are injected; the OpenAI model validates them again).
type azurePickedOptions struct {
	keys   []string
	values map[string]interface{}
}

func pickAzureOptions(providerOptions map[string]interface{}, shape []string) azurePickedOptions {
	var picked azurePickedOptions
	azure, _ := providerOptions["azure"].(map[string]interface{})
	for _, key := range shape {
		v, ok := azure[key]
		if !ok {
			continue
		}
		if picked.values == nil {
			picked.values = map[string]interface{}{}
		}
		picked.keys = append(picked.keys, key)
		picked.values[key] = v
	}
	return picked
}

// without returns the picked options minus key.
func (p azurePickedOptions) without(key string) azurePickedOptions {
	out := azurePickedOptions{}
	for _, k := range p.keys {
		if k == key {
			continue
		}
		if out.values == nil {
			out.values = map[string]interface{}{}
		}
		out.keys = append(out.keys, k)
		out.values[k] = p.values[k]
	}
	return out
}

// withAzureOptionsForOpenAI merges the picked azure options into
// providerOptions.openai for the OpenAI model, azure winning (TS
// withAzureOptionsForOpenAI). The caller's map is not modified.
func withAzureOptionsForOpenAI(providerOptions map[string]interface{}, picked azurePickedOptions) map[string]interface{} {
	if len(picked.keys) == 0 {
		return providerOptions
	}
	out := make(map[string]interface{}, len(providerOptions)+1)
	for k, v := range providerOptions {
		out[k] = v
	}
	merged := map[string]interface{}{}
	if openai, ok := providerOptions["openai"].(map[string]interface{}); ok {
		for k, v := range openai {
			merged[k] = v
		}
	}
	for _, k := range picked.keys {
		merged[k] = picked.values[k]
	}
	out["openai"] = merged
	return out
}

// azureUnsupportedOptionWarnings is TS unsupportedOptionWarnings: one
// "unsupported" warning per picked option.
func azureUnsupportedOptionWarnings(picked azurePickedOptions, details string) []types.Warning {
	var warnings []types.Warning
	for _, k := range picked.keys {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.azure." + k, Details: details})
	}
	return warnings
}
