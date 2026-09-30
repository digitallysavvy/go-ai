package anthropic

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// ForwardContainerIDFromLastStep searches backwards through steps to find the
// most recent Anthropic code-execution container ID and returns provider
// options that forward it to the next step. Use this inside a PrepareStep
// callback (returning it as PrepareStepOptions.ProviderOptions) to keep reusing
// the same container across multiple steps of a multi-step call.
//
// Returns nil when no step carries a container ID.
//
// Ports packages/anthropic/src/forward-anthropic-container-id-from-last-step.ts
// (forwardAnthropicContainerIdFromLastStep) at ai@7.0.118.
func ForwardContainerIDFromLastStep(steps []types.StepResult) map[string]interface{} {
	for i := len(steps) - 1; i >= 0; i-- {
		anthropicMeta, ok := steps[i].ProviderMetadata["anthropic"].(map[string]interface{})
		if !ok {
			continue
		}
		container, ok := anthropicMeta["container"].(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := container["id"].(string)
		if id == "" {
			continue
		}
		return map[string]interface{}{
			"anthropic": map[string]interface{}{
				"container": map[string]interface{}{
					"id": id,
				},
			},
		}
	}
	return nil
}
