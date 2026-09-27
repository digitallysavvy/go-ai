package codemode

import (
	"context"
	"strings"
	"testing"
)

// Ports TypeScript's code-mode/src/utils/options.test.ts ("options
// validation"). TS's cases include a non-integer (1.5) and NaN value for
// each field; Go's ExecutionPolicy fields are plain int, which cannot
// represent either, so only the negative-value cases (which TS also
// rejects, alongside zero) have a Go equivalent -- see
// resolveExecutionPolicy's doc comment for why Go additionally accepts
// zero (its "use the default" sentinel, since Go has no separate
// "unset" representation for an int field).
func TestRunCodeMode_RejectsNegativeExecutionPolicyOptions(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*ExecutionPolicy)
	}{
		{"timeoutMs", func(p *ExecutionPolicy) { p.TimeoutMs = -1 }},
		{"memoryLimitBytes", func(p *ExecutionPolicy) { p.MemoryLimitBytes = -1 }},
		{"maxStackSizeBytes", func(p *ExecutionPolicy) { p.MaxStackSizeBytes = -1 }},
		{"maxResultBytes", func(p *ExecutionPolicy) { p.MaxResultBytes = -1 }},
		{"maxConsoleOutputBytes", func(p *ExecutionPolicy) { p.MaxConsoleOutputBytes = -1 }},
		{"maxSourceBytes", func(p *ExecutionPolicy) { p.MaxSourceBytes = -1 }},
		{"maxToolInputBytes", func(p *ExecutionPolicy) { p.MaxToolInputBytes = -1 }},
		{"maxToolOutputBytes", func(p *ExecutionPolicy) { p.MaxToolOutputBytes = -1 }},
		{"maxBridgeRequests", func(p *ExecutionPolicy) { p.MaxBridgeRequests = -1 }},
		{"maxInFlightBridgeRequests", func(p *ExecutionPolicy) { p.MaxInFlightBridgeRequests = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy := &ExecutionPolicy{}
			c.apply(policy)
			_, err := RunCodeMode(context.Background(), RunInput{
				JS:      "return 'unused';",
				Tools:   ToolSet{},
				Options: &Options{ExecutionPolicy: policy},
			})
			if err == nil {
				t.Fatalf("expected an error for negative %s", c.name)
			}
			if !strings.Contains(err.Error(), "executionPolicy."+c.name) || !strings.Contains(err.Error(), "positive integer") {
				t.Fatalf("expected error to mention executionPolicy.%s must be a positive integer, got %v", c.name, err)
			}
		})
	}
}

func TestSetMaxWorkers_RejectsNegativeValues(t *testing.T) {
	err := SetMaxWorkers(-1)
	if err == nil {
		t.Fatal("expected an error for a negative maxWorkers value")
	}
	if !strings.Contains(err.Error(), "maxWorkers") || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("expected error to mention maxWorkers must be a positive integer, got %v", err)
	}
}

func TestSetMaxWorkers_AcceptsZeroAsUnlimited(t *testing.T) {
	// Unlike TypeScript's setMaxWorkers, 0 is Go's "unlimited" sentinel
	// here, not a rejected value -- see SetMaxWorkers's doc comment.
	if err := SetMaxWorkers(0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
