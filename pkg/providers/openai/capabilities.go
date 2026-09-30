package openai

import "regexp"

// LanguageModelCapabilities describes model-version-derived behavior
// differences across the OpenAI chat/completions and Responses APIs.
//
// This is a Go port of TypeScript's getOpenAILanguageModelCapabilities
// (openai-language-model-capabilities.ts, commit 34c53c0), replacing the
// old ad-hoc prefix-list functions (isReasoningModel,
// supportsNonReasoningParameters) with regex-based GPT/o-series version
// parsing that correctly classifies GPT-6+ and GPT-5.6 models.
type LanguageModelCapabilities struct {
	// IsReasoningModel reports whether the model requires the "developer"
	// system role and drops non-reasoning sampling parameters by default.
	IsReasoningModel bool

	// SystemMessageMode is "system" or "developer" (Chat Completions never
	// needs "remove"; that mode is only used by the Responses API).
	SystemMessageMode string

	// SupportsFlexProcessing reports whether serviceTier "flex" is allowed.
	SupportsFlexProcessing bool

	// SupportsPriorityProcessing reports whether serviceTier
	// "priority"/"fast" is allowed.
	SupportsPriorityProcessing bool

	// SupportsConfigurationUpdate reports whether the Responses API
	// "configuration_update" item (reasoningEffortUpdate) is supported.
	SupportsConfigurationUpdate bool

	// SupportsAsyncToolCalling reports whether the model supports the
	// Responses API "async" flag on tools/tool calls.
	SupportsAsyncToolCalling bool

	// SupportedReasoningEfforts, when non-nil, restricts the set of valid
	// reasoning effort strings (GPT-6+ models). A nil slice means no
	// restriction (any effort string, or none, is accepted as before).
	SupportedReasoningEfforts []string

	// SupportsNonReasoningParameters reports whether temperature/topP/
	// logprobs may be sent when reasoningEffort is "none" (GPT-5.1+, but
	// not GPT-6+, which uses SupportedReasoningEfforts instead).
	SupportsNonReasoningParameters bool
}

var (
	oSeriesVersionRe = regexp.MustCompile(`^o(\d+)(?:-|$)`)
	gptVersionRe     = regexp.MustCompile(`^gpt-(\d+)(?:\.(\d+))?(?:-(.+))?$`)
)

type gptVersion struct {
	major   int
	minor   int
	hasMin  bool
	variant string
}

// GetLanguageModelCapabilities classifies modelID the same way TypeScript's
// getOpenAILanguageModelCapabilities does.
func GetLanguageModelCapabilities(modelID string) LanguageModelCapabilities {
	oVersion, hasOVersion := getOSeriesVersion(modelID)
	gv, hasGV := getGptVersion(modelID)

	isGptChatModel := hasGV && !gv.hasMin && hasPrefixVariant(gv.variant, "chat")
	isGptNanoModel := hasGV && hasPrefixVariant(gv.variant, "nano")
	isGpt6OrLaterModel := hasGV && gv.major >= 6

	isGpt6SolOrLuna := modelID == "gpt-6-sol" || modelID == "gpt-6-luna"

	supportsFlexProcessing := (hasOVersion && oVersion >= 3) ||
		(hasGV && gv.major >= 5 && !isGptChatModel)

	supportsPriorityProcessing := hasPrefix(modelID, "gpt-4") ||
		(hasGV && gv.major >= 5 && !isGptNanoModel && !isGptChatModel) ||
		(hasOVersion && oVersion >= 3)

	isReasoningModel := hasOVersion ||
		(hasGV && gv.major >= 5 && !isGptChatModel)

	// https://platform.openai.com/docs/guides/latest-model#gpt-5-1-parameter-compatibility
	supportsNonReasoningParameters := !isGpt6OrLaterModel && hasGV &&
		(gv.major > 5 || (gv.major == 5 && gv.minor >= 1))

	systemMessageMode := "system"
	if isReasoningModel {
		systemMessageMode = "developer"
	}

	var supportedReasoningEfforts []string
	switch {
	case isGpt6SolOrLuna:
		supportedReasoningEfforts = []string{"none", "low", "medium", "high", "xhigh", "max"}
	case isGpt6OrLaterModel:
		supportedReasoningEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	}

	return LanguageModelCapabilities{
		IsReasoningModel:               isReasoningModel,
		SystemMessageMode:              systemMessageMode,
		SupportsFlexProcessing:         supportsFlexProcessing,
		SupportsPriorityProcessing:     supportsPriorityProcessing,
		SupportsConfigurationUpdate:    isGpt6OrLaterModel,
		SupportsAsyncToolCalling:       isGpt6OrLaterModel,
		SupportedReasoningEfforts:      supportedReasoningEfforts,
		SupportsNonReasoningParameters: supportsNonReasoningParameters,
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasPrefixVariant(variant, prefix string) bool {
	if variant == "" {
		return false
	}
	return hasPrefix(variant, prefix)
}

func getOSeriesVersion(modelID string) (int, bool) {
	m := oSeriesVersionRe.FindStringSubmatch(modelID)
	if m == nil {
		return 0, false
	}
	return atoiSafe(m[1]), true
}

func getGptVersion(modelID string) (gptVersion, bool) {
	m := gptVersionRe.FindStringSubmatch(modelID)
	if m == nil {
		return gptVersion{}, false
	}
	gv := gptVersion{major: atoiSafe(m[1]), variant: m[3]}
	if m[2] != "" {
		gv.minor = atoiSafe(m[2])
		gv.hasMin = true
	}
	return gv, true
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
