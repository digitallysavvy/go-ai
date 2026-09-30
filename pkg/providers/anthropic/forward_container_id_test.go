package anthropic

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the behavior described in
// packages/anthropic/src/forward-anthropic-container-id-from-last-step.ts
// (ai@7.0.118): search backwards through steps for the most recent
// providerMetadata.anthropic.container.id and forward it as provider options.

func TestForwardContainerIDFromLastStep_FindsMostRecent(t *testing.T) {
	steps := []types.StepResult{
		{
			StepNumber: 0,
			ProviderMetadata: map[string]interface{}{
				"anthropic": map[string]interface{}{
					"container": map[string]interface{}{"id": "container_early"},
				},
			},
		},
		{
			// Middle step has no container metadata at all.
			StepNumber: 1,
		},
		{
			StepNumber: 2,
			ProviderMetadata: map[string]interface{}{
				"anthropic": map[string]interface{}{
					"container": map[string]interface{}{"id": "container_latest"},
				},
			},
		},
	}

	got := ForwardContainerIDFromLastStep(steps)
	want := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"container": map[string]interface{}{"id": "container_latest"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ForwardContainerIDFromLastStep() = %#v, want %#v", got, want)
	}
}

func TestForwardContainerIDFromLastStep_FallsBackToEarlierStep(t *testing.T) {
	steps := []types.StepResult{
		{
			StepNumber: 0,
			ProviderMetadata: map[string]interface{}{
				"anthropic": map[string]interface{}{
					"container": map[string]interface{}{"id": "container_only"},
				},
			},
		},
		{
			// Latest step has no container id.
			StepNumber: 1,
			ProviderMetadata: map[string]interface{}{
				"anthropic": map[string]interface{}{},
			},
		},
	}

	got := ForwardContainerIDFromLastStep(steps)
	want := map[string]interface{}{
		"anthropic": map[string]interface{}{
			"container": map[string]interface{}{"id": "container_only"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ForwardContainerIDFromLastStep() = %#v, want %#v", got, want)
	}
}

func TestForwardContainerIDFromLastStep_NoContainerID(t *testing.T) {
	steps := []types.StepResult{
		{StepNumber: 0},
		{StepNumber: 1, ProviderMetadata: map[string]interface{}{"anthropic": map[string]interface{}{}}},
	}

	if got := ForwardContainerIDFromLastStep(steps); got != nil {
		t.Errorf("ForwardContainerIDFromLastStep() = %#v, want nil", got)
	}
}

func TestForwardContainerIDFromLastStep_EmptySteps(t *testing.T) {
	if got := ForwardContainerIDFromLastStep(nil); got != nil {
		t.Errorf("ForwardContainerIDFromLastStep(nil) = %#v, want nil", got)
	}
}
