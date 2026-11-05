package ai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// UnsupportedModelVersionError reports a model whose specification version
// the SDK does not support (TS AI_UnsupportedModelVersionError).
// ExperimentalDecide requires decision models of specification version "v4",
// as TS resolveDecisionModel does.
type UnsupportedModelVersionError struct {
	Version  string
	Provider string
	ModelID  string
}

// Error is the TS message, verbatim.
func (e *UnsupportedModelVersionError) Error() string {
	return fmt.Sprintf("Unsupported model version %s for provider %q and model %q. AI SDK 5 only supports models that implement specification version \"v2\".", e.Version, e.Provider, e.ModelID)
}

// ErrorName is the TS error name.
func (e *UnsupportedModelVersionError) ErrorName() string { return "AI_UnsupportedModelVersionError" }

// checkDecisionModelVersion is TS resolveDecisionModel's
// `model.specificationVersion !== 'v4'` check.
func checkDecisionModelVersion(m provider.DecisionModel) (provider.DecisionModel, error) {
	if v := m.SpecificationVersion(); v != "v4" {
		return nil, &UnsupportedModelVersionError{Version: v, Provider: m.Provider(), ModelID: m.ModelID()}
	}
	return m, nil
}
