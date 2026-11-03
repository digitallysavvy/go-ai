package openai

import (
	"regexp"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

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

	// SupportedReasoningEfforts, when non-nil, lists the reasoning efforts
	// the model accepts (GPT-6+ models and the standard GPT-5.x models, TS
	// openai-model-catalog reasoning.efforts). A top-level reasoning value
	// outside it is rounded to a supported effort (TS
	// resolveOpenAIReasoningEffort). A nil slice means the efforts are not
	// known, and any effort is sent as is.
	SupportedReasoningEfforts []string

	// SupportsNonReasoningParameters reports whether temperature/topP/
	// logprobs may be sent when reasoningEffort is "none" (GPT-5.1+ and
	// GPT-6 Sol/Luna and GPT-6.1 Terra, but not other GPT-6+ models).
	SupportsNonReasoningParameters bool

	// SupportsChatFunctionToolsWithReasoning reports whether the Chat
	// Completions API accepts function tools together with reasoning (TS
	// endpoints.chat.functionToolsWithReasoning). GPT-5.6 and later GPT-5.x
	// models reject the combination, so Chat Completions sends reasoning
	// effort "none" for them when function tools are present.
	SupportsChatFunctionToolsWithReasoning bool
}

// standardGpt5VariantRe matches the GPT-5 variants whose reasoning efforts
// are known (TS isStandardGpt5Model): no variant, mini/nano/sol/luna/terra
// with an optional date, or a bare date. Pro, codex and chat variants are
// excluded.
var standardGpt5VariantRe = regexp.MustCompile(`^(?:(?:mini|nano|sol|luna|terra)(?:-\d{4}-\d{2}-\d{2})?|\d{4}-\d{2}-\d{2})$`)

// gpt5ReasoningEfforts is TS getGpt5ReasoningEfforts.
func gpt5ReasoningEfforts(minor int) []string {
	switch {
	case minor == 0:
		return []string{"minimal", "low", "medium", "high"}
	case minor == 1:
		return []string{"none", "low", "medium", "high"}
	case minor < 6:
		return []string{"none", "low", "medium", "high", "xhigh"}
	default:
		return []string{"none", "low", "medium", "high", "xhigh", "max"}
	}
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

	// GPT-6 and later models that accept reasoning effort "none" (TS
	// catalog configurableNonReasoning: gpt-6-sol, gpt-6-luna,
	// gpt-6.1-terra).
	isGpt6OrLaterModelWithNoneEffort := modelID == "gpt-6-sol" || modelID == "gpt-6-luna" || modelID == "gpt-6.1-terra"
	isStandardGpt5Model := hasGV && gv.major == 5 &&
		(gv.variant == "" || standardGpt5VariantRe.MatchString(gv.variant))

	supportsFlexProcessing := (hasOVersion && oVersion >= 3) ||
		(hasGV && gv.major >= 5 && !isGptChatModel)

	supportsPriorityProcessing := hasPrefix(modelID, "gpt-4") ||
		(hasGV && gv.major >= 5 && !isGptNanoModel && !isGptChatModel) ||
		(hasOVersion && oVersion >= 3)

	isReasoningModel := hasOVersion ||
		(hasGV && gv.major >= 5 && !isGptChatModel)

	// https://platform.openai.com/docs/guides/latest-model#gpt-5-1-parameter-compatibility
	// GPT-5.1 through GPT-5.x, GPT-6 Sol/Luna and GPT-6.1 Terra support
	// temperature, topP, and logprobs when reasoningEffort is none.
	supportsNonReasoningParameters := isGpt6OrLaterModelWithNoneEffort ||
		(!isGpt6OrLaterModel && hasGV &&
			(gv.major > 5 || (gv.major == 5 && gv.minor >= 1)))

	systemMessageMode := "system"
	if isReasoningModel {
		systemMessageMode = "developer"
	}

	var supportedReasoningEfforts []string
	switch {
	case isGpt6OrLaterModelWithNoneEffort:
		supportedReasoningEfforts = []string{"none", "low", "medium", "high", "xhigh", "max"}
	case isGpt6OrLaterModel:
		supportedReasoningEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	case isStandardGpt5Model:
		supportedReasoningEfforts = gpt5ReasoningEfforts(gv.minor)
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
		// GPT-5.6 rejects function tools with reasoning in Chat Completions.
		SupportsChatFunctionToolsWithReasoning: !(hasGV && gv.major == 5 && gv.minor >= 6),
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

// allOpenAIReasoningEfforts are the efforts assumed for a model whose
// efforts are not known (TS resolve-openai-reasoning-effort.ts).
var allOpenAIReasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// resolveOpenAIReasoningEffort ports TS resolveOpenAIReasoningEffort: a
// provider-option effort is returned unchanged; otherwise the top-level
// reasoning value is mapped onto supportedReasoningEfforts (all efforts when
// nil) with providerutils.MapReasoningToProviderEffort, rounding an
// unsupported level to the nearest supported one with a "compatibility"
// warning. "" means no effort is sent.
func resolveOpenAIReasoningEffort(providerReasoningEffort *string, reasoning *types.ReasoningLevel, supportedReasoningEfforts []string, warnings *[]types.Warning) string {
	efforts := supportedReasoningEfforts
	if efforts == nil {
		efforts = allOpenAIReasoningEfforts
	}
	effortMap := make(map[types.ReasoningLevel]string, len(efforts))
	for _, e := range efforts {
		effortMap[types.ReasoningLevel(e)] = e
	}
	effort, _ := providerutils.MapReasoningToProviderEffort(reasoning, effortMap, providerReasoningEffort, warnings)
	return effort
}
