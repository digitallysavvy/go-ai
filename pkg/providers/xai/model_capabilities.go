package xai

import (
	"regexp"
	"slices"
)

// xaiLanguageModelRow is one row of the TS language model matrix
// (languageModelDefinitions in packages/xai/src/xai-model-catalog.ts at
// a457990a33, TS 565db6d2c8). Every listed model fills in every column and
// nothing is derived from the model ID. Only the columns with a Go consumer
// are used (reasoning efforts); documented and image are kept so the row
// matches the TS source, which TestPortprovbXAICapabilityMatrixMatchesTS
// compares.
type xaiLanguageModelRow struct {
	documented bool
	// image is "true", "false" or "unknown".
	image string
	// reasons is false for a model that does not reason and rejects a
	// reasoning effort (TS reasoningEfforts: false).
	reasons bool
	// reasoningEfforts are the efforts the model accepts; an empty list on a
	// model that reasons means its effort cannot be set.
	reasoningEfforts []string
}

// xaiLanguageModelMatrix is the language model matrix: model IDs by
// capability. To add a model, add a row and fill in every column.
var xaiLanguageModelMatrix = map[string]xaiLanguageModelRow{
	"grok-4.7":                    {documented: true, image: "true", reasoningEfforts: []string{"low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4.6":                    {documented: true, image: "true", reasoningEfforts: []string{"low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4.5":                    {documented: true, image: "true", reasoningEfforts: []string{"low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4.20-reasoning":         {documented: true, image: "true", reasoningEfforts: []string{}, reasons: true},
	"grok-4.20-non-reasoning":     {documented: true, image: "true", reasoningEfforts: nil, reasons: false},
	"grok-4-1-fast-reasoning":     {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4-1-fast-non-reasoning": {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4-1":                    {documented: true, image: "false", reasoningEfforts: []string{"none", "low", "medium", "high"}, reasons: true},
	"grok-4-fast-reasoning":       {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4-fast-non-reasoning":   {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-code-fast-1":            {documented: true, image: "false", reasoningEfforts: []string{"none", "low", "medium", "high"}, reasons: true},
	"grok-4":                      {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-3":                      {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-3-mini":                 {documented: true, image: "true", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-4.3":                    {documented: false, image: "unknown", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
	"grok-latest":                 {documented: false, image: "unknown", reasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}, reasons: true},
}

// Family rules, used only for model IDs that are not listed in the matrix.
// These 4.20 variants reject reasoning effort, including "none". Multi-agent
// variants deliberately retain the configurable-effort fallback.
var xaiUnlistedModelsWithoutReasoningEffortRe = regexp.MustCompile(`^grok-4\.20(-\d{4})?-(non-)?reasoning$`)

// 4.20 predates 4.5 despite its version number. The lookahead of the TS
// pattern (?!\d) is written as "a non-digit or the end".
var xaiUnlistedModelsWithoutNoneReasoningEffortRe = regexp.MustCompile(`^grok-(?:4\.[5-9](?:[^0-9]|$)|(?:[5-9]|[1-9][0-9]+)(?:[.-]|$))`)

// xaiPortableEfforts are the reasoning efforts the SDK sends for portable
// (top-level) reasoning (TS getXaiLanguageModelEntry(...).reasoning
// .portableEfforts): the model's own row, or the family rules for an unlisted
// ID. Empty: portable reasoning is not sent.
func xaiPortableEfforts(modelID string) []string {
	if row, ok := xaiLanguageModelMatrix[modelID]; ok {
		return row.reasoningEfforts
	}
	switch {
	case xaiUnlistedModelsWithoutReasoningEffortRe.MatchString(modelID):
		return nil
	case xaiUnlistedModelsWithoutNoneReasoningEffortRe.MatchString(modelID):
		return []string{"low", "medium", "high"}
	default:
		return []string{"none", "low", "medium", "high"}
	}
}

// supportsReasoningEffort reports whether modelID accepts a portable
// reasoning effort at all (TS supportsReasoningEffort).
func supportsReasoningEffort(modelID string) bool {
	return len(xaiPortableEfforts(modelID)) > 0
}

// supportsNoneReasoningEffort ports TS supportsNoneReasoningEffort.
func supportsNoneReasoningEffort(modelID string) bool {
	return slices.Contains(xaiPortableEfforts(modelID), "none")
}
