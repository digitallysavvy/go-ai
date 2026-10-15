package providerutils

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MapReasoningToProviderEffort ports TS provider-utils
// mapReasoningToProviderEffort: it maps a portable reasoning level to a
// provider effort string. A level missing from effortMap yields an
// "unsupported" warning and ok=false; a level mapped to a different string
// yields a "compatibility" warning.
func MapReasoningToProviderEffort(reasoning types.ReasoningLevel, effortMap map[types.ReasoningLevel]string, warnings *[]types.Warning) (string, bool) {
	mapped, ok := effortMap[reasoning]
	if !ok {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: fmt.Sprintf("reasoning %q is not supported by this model.", reasoning),
		})
		return "", false
	}
	if mapped != string(reasoning) {
		details := fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", reasoning, mapped)
		*warnings = append(*warnings, types.Warning{Type: "compatibility", Feature: "reasoning", Details: details})
	}
	return mapped, true
}
