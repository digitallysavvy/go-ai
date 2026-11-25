package ai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// StoppedBy says why a generation stopped its tool loop (TS StoppedBy,
// 7760e33531). Unlike the finish reason, which describes the last model call,
// it tells whether the model, a stop condition, a tool without an Execute
// function, or a tool approval ended the loop.
type StoppedBy string

const (
	// StoppedByModel: the model finished without tool calls that continue
	// the loop, for example it gave a final answer, hit the output limit, or
	// its call was interrupted.
	StoppedByModel StoppedBy = "model"
	// StoppedByStopCondition: a StopWhen condition was met.
	StoppedByStopCondition StoppedBy = "stop-condition"
	// StoppedByToolWithoutExecute: a tool call needs a result from a tool
	// without an Execute function.
	StoppedByToolWithoutExecute StoppedBy = "tool-without-execute"
	// StoppedByApproval: a tool call needs an approval.
	StoppedByApproval StoppedBy = "approval"
)

// pendingToolCallStoppedBy is TS getPendingToolCallStoppedBy: the loop stopped
// with client tool calls still waiting for a result.
func pendingToolCallStoppedBy(step types.StepResult) StoppedBy {
	if !isToolExecutionAllowedFinishReason(step.FinishReason) {
		return StoppedByModel
	}
	if stepHasPendingApproval(step) {
		return StoppedByApproval
	}
	return StoppedByToolWithoutExecute
}

// modelStoppedBy is TS getModelStoppedBy: the model ended the loop, unless
// the step still holds an approval request.
func modelStoppedBy(step types.StepResult) StoppedBy {
	if stepHasPendingApproval(step) {
		return StoppedByApproval
	}
	return StoppedByModel
}

func stepHasPendingApproval(step types.StepResult) bool {
	for _, part := range step.Content {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			if !p.IsAutomatic {
				return true
			}
		case *types.ToolApprovalRequestContent:
			if p != nil && !p.IsAutomatic {
				return true
			}
		}
	}
	return false
}
