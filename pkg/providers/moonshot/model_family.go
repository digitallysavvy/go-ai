package moonshot

import "strings"

// ModelFamily identifies the Moonshot model family a given model ID belongs
// to. Reasoning/thinking options, tool-choice restrictions, and sampling
// option support all vary by family, mirroring TS getMoonshotAIModelFamily.
type ModelFamily string

const (
	FamilyKimiK25    ModelFamily = "kimi-k2.5"
	FamilyKimiK26    ModelFamily = "kimi-k2.6"
	FamilyKimiK27    ModelFamily = "kimi-k2.7"
	FamilyKimiK3     ModelFamily = "kimi-k3"
	FamilyMoonshotV1 ModelFamily = "moonshot-v1"
	FamilyUnknown    ModelFamily = "unknown"
)

// GetModelFamily classifies a Moonshot model ID into its family. Mirrors TS
// getMoonshotAIModelFamily in moonshotai-chat-options.ts exactly.
func GetModelFamily(modelID string) ModelFamily {
	switch modelID {
	case "kimi-k2.5":
		return FamilyKimiK25
	case "kimi-k2.6":
		return FamilyKimiK26
	case "kimi-k2.7-code", "kimi-k2.7-code-highspeed":
		return FamilyKimiK27
	case "kimi-k3":
		return FamilyKimiK3
	}
	if strings.HasPrefix(modelID, "moonshot-v1-") {
		return FamilyMoonshotV1
	}
	return FamilyUnknown
}

// IsKimiModel reports whether modelID belongs to a "kimi-*" family. Mirrors
// TS isMoonshotAIKimiModel.
func IsKimiModel(modelID string) bool {
	return strings.HasPrefix(string(GetModelFamily(modelID)), "kimi-")
}

// SupportsStructuredOutputs reports whether native json_schema response
// format is supported for modelID. Mirrors TS getModelStructuredOutputSupport
// in moonshotai-provider.ts: all "kimi-k*" models, plus the specific
// non-preview and vision-preview Moonshot V1 models.
func SupportsStructuredOutputs(modelID string) bool {
	if strings.HasPrefix(modelID, "kimi-k") {
		return true
	}
	switch modelID {
	case "moonshot-v1-8k",
		"moonshot-v1-32k",
		"moonshot-v1-128k",
		"moonshot-v1-auto",
		"moonshot-v1-8k-vision-preview",
		"moonshot-v1-32k-vision-preview",
		"moonshot-v1-128k-vision-preview":
		return true
	}
	return false
}
