package codemode

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS validates the policy in `run`'s normalizeOptions: alphabetical by run
// limit name, with "must be a positive integer." / "must be at most
// 2147483647." translated to executionPolicy.* paths.
func TestExecutionPolicyValidationMatchesRunOrderAndText(t *testing.T) {
	cases := []struct {
		policy ExecutionPolicy
		want   string
	}{
		{ExecutionPolicy{TimeoutMs: -1, CPUTimeoutMs: -1}, "executionPolicy.cpuTimeoutMs must be a positive integer."},
		{ExecutionPolicy{TimeoutMs: -1, MemoryLimitBytes: -1}, "executionPolicy.memoryLimitBytes must be a positive integer."},
		{ExecutionPolicy{MaxSourceBytes: -1, MaxToolInputBytes: -1}, "executionPolicy.maxToolInputBytes must be a positive integer."},
		{ExecutionPolicy{MaxResultBytes: 1 << 40}, "executionPolicy.maxResultBytes must be at most 2147483647."},
		{ExecutionPolicy{TimeoutMs: 2_147_483_648}, "executionPolicy.timeoutMs must be at most 2147483647."},
	}
	for _, c := range cases {
		p := c.policy
		_, err := RunCodeMode(context.Background(), RunInput{JS: "return 1;", Options: &Options{ExecutionPolicy: &p}})
		if err == nil || err.Error() != c.want {
			t.Errorf("policy %+v: err = %v, want %q", c.policy, err, c.want)
		}
	}
}

// TS checks the source size before the runner validates the limits.
func TestSourceSizeCheckedBeforePolicyValidation(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return 'too long';",
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxSourceBytes: 5, TimeoutMs: -1}},
	})
	var tooLarge *SourceTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %#v (%v), want *SourceTooLargeError", err, err)
	}
	// An invalid maxSourceBytes skips the size check (TS isValidRunLimit)
	// and is reported by the validation instead.
	_, err = RunCodeMode(context.Background(), RunInput{
		JS:      "return 'too long';",
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxSourceBytes: -1}},
	})
	if err == nil || err.Error() != "executionPolicy.maxSourceBytes must be a positive integer." {
		t.Fatalf("err = %v", err)
	}
}

func TestSetMaxWorkersErrorText(t *testing.T) {
	if err := SetMaxWorkers(-1); err == nil || err.Error() != "maxWorkers must be a positive integer." {
		t.Fatalf("err = %v", err)
	}
}

func echoTools() ToolSet {
	return ToolSet{"echo": {
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
}

// The bridge limit message is `run`'s (manager.js admitBridgeRequest).
func TestBridgeLimitErrorUsesRunText(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "await tools.echo({}); return await tools.echo({});",
		Tools:   echoTools(),
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxBridgeRequests: 1}},
	})
	var limit *BridgeLimitError
	if !errors.As(err, &limit) || limit.Error() != "JavaScript runtime exceeded the 1 bridge request limit." {
		t.Fatalf("err = %#v (%v)", err, err)
	}
}

func TestInFlightBridgeLimitErrorUsesRunText(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "should not run", nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await Promise.all([tools.guarded({}), tools.guarded({})]);",
		Tools: tools,
		Options: &Options{
			Approval:        &ApprovalOptions{Mode: ApprovalModeInterrupt},
			ExecutionPolicy: &ExecutionPolicy{MaxInFlightBridgeRequests: 1},
		},
	})
	var limit *BridgeLimitError
	if !errors.As(err, &limit) || limit.Error() != "JavaScript runtime exceeded the 1 in-flight bridge request limit." {
		t.Fatalf("err = %#v (%v)", err, err)
	}
}

// The detached-request message and counts are `run`'s (manager.js).
func TestDetachedBridgeRequestErrorUsesRunText(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "should not run", nil
		},
	}, "echo": echoTools()["echo"]}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "await tools.echo({}); tools.guarded({}); return 'done';",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	var detached *DetachedBridgeRequestError
	if !errors.As(err, &detached) || detached.Error() != "JavaScript runtime returned while 1 bridge request(s) were still in flight." {
		t.Fatalf("err = %#v (%v)", err, err)
	}
	d, _ := detached.Details.(map[string]interface{})
	if d["inFlightBridgeRequests"] != 1 || d["totalBridgeRequests"] != 2 || d["toolName"] != "guarded" {
		t.Fatalf("details = %#v", detached.Details)
	}
}

// TS serializes the input against maxToolInputBytes before looking the
// tool up.
func TestOversizedInputToUnknownToolReportsSizeError(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      `return await tools.nope({a: "xxxxxxxxxx"});`,
		Tools:   echoTools(),
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxToolInputBytes: 5}},
	})
	if err == nil || err.Error() != `Tool "nope" input exceeds the 5 byte size limit.` {
		t.Fatalf("err = %v", err)
	}
}
