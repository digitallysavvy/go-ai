package telemetry

import "testing"

// TS 4d6d830c14 gen-ai-format-messages.test.ts: the WorkflowAgent root
// operation maps to invoke_agent.
func TestR85carryWorkflowAgentOperationName(t *testing.T) {
	if got := mapOperationName("ai.workflowAgent.stream"); got != "invoke_agent" {
		t.Fatalf("mapOperationName(ai.workflowAgent.stream) = %q, want invoke_agent", got)
	}
}
