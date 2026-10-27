package codemode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23812: executionPolicy.cpuTimeoutMs.

func cpuPolicy(cpuMs, wallMs int) *Options {
	return &Options{ExecutionPolicy: &ExecutionPolicy{CPUTimeoutMs: cpuMs, TimeoutMs: wallMs}}
}

func TestRunCodeModeMapsCPUTimeoutToCPUTimeoutError(t *testing.T) {
	// Warm the engine up first so only the loop is timed (setup is slow under -race).
	if _, err := RunCodeMode(context.Background(), RunInput{JS: `return 1;`, Tools: ToolSet{}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      `while (true) {}`,
		Tools:   ToolSet{},
		Options: cpuPolicy(50, 10_000),
	})
	var cpuErr *CPUTimeoutError
	if !errors.As(err, &cpuErr) {
		t.Fatalf("err = %T %v, want *CPUTimeoutError", err, err)
	}
	if cpuErr.Code != "CODE_MODE_CPU_TIMEOUT" {
		t.Fatalf("code = %q", cpuErr.Code)
	}
	if d, _ := cpuErr.Details.(map[string]interface{}); d["cpuTimeoutMs"] != 50 {
		t.Fatalf("details = %#v", cpuErr.Details)
	}
	if !strings.Contains(cpuErr.Error(), "CPU time limit of 50ms") {
		t.Fatalf("message = %q", cpuErr.Error())
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v: the CPU limit did not stop the loop promptly", elapsed)
	}
}

func TestRunCodeModeWallTimeoutWinsWhenLowerThanCPUTimeout(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      `while (true) {}`,
		Tools:   ToolSet{},
		Options: cpuPolicy(10_000, 100),
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("err = %T %v, want *TimeoutError", err, err)
	}
}

func TestRunCodeModeToolWaitDoesNotCountTowardCPUTimeout(t *testing.T) {
	tools := ToolSet{
		"wait": {
			Name:       "wait",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-time.After(400 * time.Millisecond):
				case <-ctx.Done():
				}
				return "done", nil
			},
		},
	}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:      `return await tools.wait({});`,
		Tools:   tools,
		Options: cpuPolicy(100, 10_000),
	})
	if err != nil || got != "done" {
		t.Fatalf("got %v, err %v; a 400ms tool wait must not exhaust a 100ms CPU budget", got, err)
	}
}

func TestRunCodeModeCPUTimeoutCountsWorkAroundToolCalls(t *testing.T) {
	tools := ToolSet{
		"noop": {
			Name:       "noop",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return 1, nil
			},
		},
	}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      `await tools.noop({}); while (true) {}`,
		Tools:   tools,
		Options: cpuPolicy(100, 10_000),
	})
	var cpuErr *CPUTimeoutError
	if !errors.As(err, &cpuErr) {
		t.Fatalf("err = %T %v, want *CPUTimeoutError", err, err)
	}
}

func TestExecutionPolicyCPUTimeoutDefaultsAndValidation(t *testing.T) {
	r, err := resolveExecutionPolicy(nil)
	if err != nil || r.CPUTimeoutMs != DefaultCPUTimeoutMs || DefaultCPUTimeoutMs != 10_000 {
		t.Fatalf("default = %d, err %v", r.CPUTimeoutMs, err)
	}
	if _, err := resolveExecutionPolicy(&ExecutionPolicy{CPUTimeoutMs: -1}); err == nil || !strings.Contains(err.Error(), "executionPolicy.cpuTimeoutMs must be a positive integer") {
		t.Fatalf("err = %v", err)
	}
	if _, err := resolveExecutionPolicy(&ExecutionPolicy{CPUTimeoutMs: 2_147_483_648}); err == nil {
		t.Fatal("expected a limit above the run maximum to be rejected")
	}
}
