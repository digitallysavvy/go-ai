package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/aibridge"
)

// r62: every pkg/internal/aibridge helper is wired by this package's init, so
// pkg/agent and pkg/workflow never call a nil function.
func TestR62AIBridgeIsWired(t *testing.T) {
	for name, set := range map[string]bool{
		"ProviderToolResultsFromContent": aibridge.ProviderToolResultsFromContent != nil,
		"MessagesForModel":               aibridge.MessagesForModel != nil,
		"ValidateToolResultParts":        aibridge.ValidateToolResultParts != nil,
		"ResolveContentFiles":            aibridge.ResolveContentFiles != nil,
		"CheckToolChoiceViolation":       aibridge.CheckToolChoiceViolation != nil,
		"EmptyProviderResults":           aibridge.EmptyProviderResults != nil,
		"WorkflowBlockedToolStep":        aibridge.WorkflowBlockedToolStep != nil,
		"WorkflowBlockedResponse":        aibridge.WorkflowBlockedResponse != nil,
		"InvokeToolInputCallbacks":       aibridge.InvokeToolInputCallbacks != nil,
		"MarkDynamicToolResults":         aibridge.MarkDynamicToolResults != nil,
		"InvalidToolCallInput":           aibridge.InvalidToolCallInput != nil,
		"ValidateToolContext":            aibridge.ValidateToolContext != nil,
		"ResolveToolApproval":            aibridge.ResolveToolApproval != nil,
	} {
		if !set {
			t.Errorf("aibridge.%s is not wired", name)
		}
	}
}
