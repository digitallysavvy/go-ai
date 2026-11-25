package deepseek

import "regexp"

// DeepSeek chat model IDs.
//
// See https://api-docs.deepseek.com/quick_start/pricing for the latest
// pricing and model list. The model ID field also accepts any other string,
// so future model IDs work without an SDK update.
const (
	// ModelDeepSeekFlash is the unversioned alias for DeepSeek's latest Flash
	// model. It currently serves DeepSeek V4.1 Flash.
	ModelDeepSeekFlash = "deepseek-flash"

	// ModelDeepSeekV4Flash is the versioned DeepSeek V4 Flash model.
	ModelDeepSeekV4Flash = "deepseek-v4-flash"

	// ModelDeepSeekV4Pro is the versioned DeepSeek V4 Pro model.
	ModelDeepSeekV4Pro = "deepseek-v4-pro"

	// ModelDeepSeekV4FlashVisionExp is the experimental DeepSeek V4 Flash
	// model with image input support.
	ModelDeepSeekV4FlashVisionExp = "deepseek-v4-flash-vision-exp"

	// ModelDeepSeekChat is the retired DeepSeek V3-generation chat alias.
	// It remains functional through the DeepSeek API but is superseded by the
	// V4 models above, which enable thinking mode by default.
	//
	// Deprecated: use ModelDeepSeekFlash or another V4 model instead.
	ModelDeepSeekChat = "deepseek-chat"

	// ModelDeepSeekReasoner is the retired DeepSeek R1-generation reasoning
	// alias. It remains functional through the DeepSeek API but is superseded
	// by the V4 models above, which enable thinking mode by default.
	//
	// Deprecated: use ModelDeepSeekFlash or another V4 model instead.
	ModelDeepSeekReasoner = "deepseek-reasoner"
)

// isDeepSeekV4Model reports whether modelID is a DeepSeek V4-generation model
// (thinking mode on by default, `reasoning_content` required on every
// assistant turn).
//
// DeepSeek publishes V4 models under both versioned ids (deepseek-v4-pro,
// deepseek-v4-flash-*) and unversioned aliases (deepseek-flash, currently
// serving V4.1 Flash). Known legacy aliases (deepseek-chat, deepseek-reasoner)
// and pre-V4 version IDs (deepseek-v1..v3) retain their previous behavior;
// unrecognized IDs inherit the current protocol.
func isDeepSeekV4Model(modelID string) bool {
	return !deepSeekLegacyModelRe.MatchString(modelID)
}

// deepSeekLegacyModelRe matches the legacy aliases and pre-V4 version IDs
// that keep their previous behavior (TS #22493). RE2 has no lookahead, so the
// trailing delimiter is consumed.
var deepSeekLegacyModelRe = regexp.MustCompile(`(^|[/.])deepseek-(?:chat|reasoner|v[1-3](?:\.\d+)?)(?:[-/:.]|$)`)
