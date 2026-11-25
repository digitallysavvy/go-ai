package openai

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModelCapabilities describes model-version-derived behavior
// differences across the OpenAI chat/completions and Responses APIs.
//
// This is a Go port of TypeScript's getOpenAILanguageModelCapabilities
// (openai-model-catalog.ts). Since TS c3ee424a70 the capabilities are declared
// per model ID in a matrix (capability_matrix.go) instead of derived from the
// model ID.
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

	// SupportsPromptCacheRetention reports whether promptCacheRetention is
	// sent (TS promptCache.retention); GPT-6 and later drop it.
	SupportsPromptCacheRetention bool

	// SupportsChatTemperatureWithoutReasoning reports whether Chat
	// Completions accepts temperature on a non-reasoning model (TS
	// endpoints.chat.temperatureWithoutReasoning); the search preview models
	// reject it.
	SupportsChatTemperatureWithoutReasoning bool

	// ResponsesLogprobsRequireNonReasoning reports whether the Responses API
	// only accepts logprobs on a non-reasoning model (TS
	// endpoints.responses.logprobsRequireNonReasoning).
	ResponsesLogprobsRequireNonReasoning bool
}

var (
	oSeriesIDRe       = regexp.MustCompile(`^o\d+(?:-|$)`)
	gptIDRe           = regexp.MustCompile(`^gpt-(\d+)(\.\d+)?(?:-([^\n\r\x{2028}\x{2029}]+))?$`)
	datedSnapshotIDRe = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
)

// isUnlistedReasoningModelID is the only capability decision made from a model
// ID (TS isUnlistedReasoningModelId): whether an unlisted ID is an OpenAI
// reasoning model (o-series, or GPT-5 and later except the GPT-5 chat models).
// Reasoning models need a different request format, so new OpenAI models keep
// working before they are added to the matrix.
func isUnlistedReasoningModelID(modelID string) bool {
	if oSeriesIDRe.MatchString(modelID) {
		return true
	}
	m := gptIDRe.FindStringSubmatch(modelID)
	if m == nil {
		return false
	}
	major, err := strconv.ParseFloat(m[1], 64)
	if err != nil && !math.IsInf(major, 0) {
		return false
	}
	return major >= 5 && (m[2] != "" || !strings.HasPrefix(m[3], "chat"))
}

// languageModelRow resolves a model ID to its matrix row (TS
// getOpenAILanguageModelEntry): the model's own row, else the row of the
// model without a dated snapshot suffix, else the generic reasoning or
// non-reasoning row.
func languageModelRow(modelID string) openAILanguageModelRow {
	baseModelID := datedSnapshotIDRe.ReplaceAllString(modelID, "")
	for _, id := range []string{modelID, baseModelID} {
		if row, ok := openAILanguageModelMatrix[id]; ok {
			return row
		}
	}
	if isUnlistedReasoningModelID(modelID) {
		return openAIUnknownReasoningModelRow
	}
	return openAIUnknownModelRow
}

// GetLanguageModelCapabilities returns the capabilities of modelID like TS
// getOpenAILanguageModelCapabilities: the model's row of the capability
// matrix, or a generic row for an unlisted ID.
func GetLanguageModelCapabilities(modelID string) LanguageModelCapabilities {
	row := languageModelRow(modelID)
	var efforts []string
	if row.reasoningEfforts != nil {
		efforts = append([]string{}, row.reasoningEfforts...)
	}
	return LanguageModelCapabilities{
		IsReasoningModel:                        row.reasoningEfforts != nil,
		SystemMessageMode:                       row.systemMessageMode,
		SupportsFlexProcessing:                  row.flexProcessing,
		SupportsPriorityProcessing:              row.priorityProcessing,
		SupportsConfigurationUpdate:             row.configurationUpdates,
		SupportsAsyncToolCalling:                row.asyncTools,
		SupportedReasoningEfforts:               efforts,
		SupportsNonReasoningParameters:          row.samplingAtReasoningEffortNone,
		SupportsChatFunctionToolsWithReasoning:  row.chatFunctionToolsWithReasoning,
		SupportsPromptCacheRetention:            row.promptCacheRetention,
		SupportsChatTemperatureWithoutReasoning: row.chatTemperature,
		ResponsesLogprobsRequireNonReasoning:    row.responsesLogprobsRequireNonReasoning,
	}
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
