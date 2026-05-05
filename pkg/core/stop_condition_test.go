package core

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestCoreStopConditionHelpers(t *testing.T) {
	if reason := IsStepCount(2)(StopConditionState{Steps: []types.StepResult{{}, {}}}); reason == "" {
		t.Fatal("expected IsStepCount to stop at exact step count")
	}
	if reason := IsLoopFinished()(StopConditionState{Steps: []types.StepResult{{}}}); reason != "" {
		t.Fatalf("expected IsLoopFinished to continue, got %q", reason)
	}
	if reason := HasToolCall("done", "finish")(StopConditionState{Steps: []types.StepResult{{ToolCalls: []types.ToolCall{{ToolName: "finish"}}}}}); reason == "" {
		t.Fatal("expected HasToolCall to stop for matching tool")
	}
}
