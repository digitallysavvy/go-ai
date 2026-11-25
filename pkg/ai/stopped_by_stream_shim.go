// TEMPORARY: the integrator deletes this file when merging oct27-port-stream; pkg/ai/stopped_by.go (gen) is the real one.
package ai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// StoppedBy says why a generation stopped its tool loop.
type StoppedBy string

const (
	// StoppedByModel: the model finished without tool calls that continue the loop.
	StoppedByModel StoppedBy = "model"
	// StoppedByStopCondition: a StopWhen condition was met.
	StoppedByStopCondition StoppedBy = "stop-condition"
	// StoppedByToolWithoutExecute: a tool call needs a result from a tool without Execute.
	StoppedByToolWithoutExecute StoppedBy = "tool-without-execute"
	// StoppedByApproval: a tool call needs an approval.
	StoppedByApproval StoppedBy = "approval"
)

func modelStoppedBy(step types.StepResult) StoppedBy {
	for _, part := range step.Content {
		if req, ok := part.(types.ToolApprovalRequestContent); ok && !req.IsAutomatic {
			return StoppedByApproval
		}
	}
	return StoppedByModel
}

func pendingToolCallStoppedBy(step types.StepResult) StoppedBy {
	if !isToolExecutionAllowedFinishReason(step.FinishReason) {
		return StoppedByModel
	}
	if modelStoppedBy(step) == StoppedByApproval {
		return StoppedByApproval
	}
	return StoppedByToolWithoutExecute
}
