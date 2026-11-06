package providerutils

import (
	"fmt"
	"math"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// reasoningLevels are the portable reasoning levels in ascending order
// (TS provider-utils REASONING_LEVELS). "none" and "provider-default" are not
// levels.
var reasoningLevels = []types.ReasoningLevel{
	types.ReasoningMinimal,
	types.ReasoningLow,
	types.ReasoningMedium,
	types.ReasoningHigh,
	types.ReasoningXHigh,
	types.ReasoningMax,
}

// IsCustomReasoning ports TS provider-utils isCustomReasoning: it reports
// whether the caller chose a reasoning value, that is a level or "none".
// A nil pointer, "provider-default" and the empty string (Go's unset value)
// are not custom.
func IsCustomReasoning(reasoning *types.ReasoningLevel) bool {
	return reasoning != nil && *reasoning != types.ReasoningDefault && *reasoning != ""
}

// MapReasoningToProviderEffort ports TS provider-utils
// mapReasoningToProviderEffort: it maps the top-level reasoning value to a
// provider effort string.
//
// A non-nil providerOptionsEffort (the effort set in provider options) is
// returned unchanged, without validation or warnings. Otherwise a reasoning
// value that is not custom (see IsCustomReasoning) returns ok=false.
//
// effortMap should hold only the values the model supports. An unsupported
// level maps to the next lower supported level, or to the next higher one if
// there is no lower level. "none" is never used as a fallback, so an
// unsupported "none" maps to the lowest supported level. A "compatibility"
// warning is appended when the effort sent differs from the requested value,
// and an "unsupported" warning (with ok=false) when effortMap is empty.
// warnings may be nil.
func MapReasoningToProviderEffort(reasoning *types.ReasoningLevel, effortMap map[types.ReasoningLevel]string, providerOptionsEffort *string, warnings *[]types.Warning) (string, bool) {
	if providerOptionsEffort != nil {
		return *providerOptionsEffort, true
	}
	if !IsCustomReasoning(reasoning) {
		return "", false
	}
	requested := *reasoning

	// The requested value first, then the lower levels from nearest to
	// lowest, then the higher levels. "none" (and any value that is not a
	// level) has index -1, so every level is a higher fallback.
	index := -1
	for i, level := range reasoningLevels {
		if level == requested {
			index = i
			break
		}
	}
	candidates := make([]types.ReasoningLevel, 0, len(reasoningLevels)+1)
	candidates = append(candidates, requested)
	for i := index - 1; i >= 0; i-- {
		candidates = append(candidates, reasoningLevels[i])
	}
	candidates = append(candidates, reasoningLevels[index+1:]...)

	for _, candidate := range candidates {
		mapped, ok := effortMap[candidate]
		if !ok {
			continue
		}
		if mapped != string(requested) {
			appendReasoningWarning(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", requested, mapped),
			})
		}
		return mapped, true
	}

	appendReasoningWarning(warnings, types.Warning{
		Type:    "unsupported",
		Feature: "reasoning",
		Details: fmt.Sprintf("reasoning %q is not supported by this model.", requested),
	})
	return "", false
}

// defaultReasoningBudgetPercentages are TS provider-utils
// DEFAULT_REASONING_BUDGET_PERCENTAGES.
var defaultReasoningBudgetPercentages = map[types.ReasoningLevel]float64{
	types.ReasoningMinimal: 0.02,
	types.ReasoningLow:     0.1,
	types.ReasoningMedium:  0.3,
	types.ReasoningHigh:    0.6,
	types.ReasoningXHigh:   0.9,
	types.ReasoningMax:     0.95,
}

// defaultMinReasoningBudget is the TS minReasoningBudget default.
const defaultMinReasoningBudget = 1024

// ReasoningBudgetOptions configures MapReasoningToProviderBudget.
type ReasoningBudgetOptions struct {
	// MaxOutputTokens is the model's maximum output tokens; the budget is a
	// percentage of it.
	MaxOutputTokens int
	// MaxReasoningBudget caps the budget.
	MaxReasoningBudget int
	// MinReasoningBudget floors the budget. nil uses the TS default 1024; an
	// explicit 0 is a floor of 0 (Gemini Flash).
	MinReasoningBudget *int
	// BudgetPercentages overrides the default percentage per level; levels
	// missing from it use the defaults.
	BudgetPercentages map[types.ReasoningLevel]float64
}

// MapReasoningToProviderBudget ports TS provider-utils
// mapReasoningToProviderBudget: it maps a reasoning level to an absolute token
// budget, MaxOutputTokens times the level's percentage (rounded half up, as
// Math.round), clamped to [MinReasoningBudget, MaxReasoningBudget].
//
// Handle "none" before calling it when the model can disable reasoning;
// otherwise "none" maps to MinReasoningBudget with a "compatibility" warning.
// warnings may be nil.
func MapReasoningToProviderBudget(reasoning types.ReasoningLevel, opts ReasoningBudgetOptions, warnings *[]types.Warning) int {
	minBudget := defaultMinReasoningBudget
	if opts.MinReasoningBudget != nil {
		minBudget = *opts.MinReasoningBudget
	}
	if reasoning == types.ReasoningNone {
		appendReasoningWarning(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "reasoning",
			Details: fmt.Sprintf("reasoning \"none\" is not supported by this model. mapped to the minimum reasoning budget of %d tokens.", minBudget),
		})
		return minBudget
	}

	percentage, ok := opts.BudgetPercentages[reasoning]
	if !ok {
		percentage = defaultReasoningBudgetPercentages[reasoning]
	}
	// Math.round rounds half up (toward +Inf), unlike math.Round.
	budget := int(math.Floor(float64(opts.MaxOutputTokens)*percentage + 0.5))
	return min(opts.MaxReasoningBudget, max(minBudget, budget))
}

func appendReasoningWarning(warnings *[]types.Warning, w types.Warning) {
	if warnings != nil {
		*warnings = append(*warnings, w)
	}
}
