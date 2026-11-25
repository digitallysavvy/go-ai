package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// workflowMaxTimeoutMs is the largest delay TS timers support (2^31 - 1 ms).
// TS rejects a longer timeout because the timer would fire after 1 ms.
const workflowMaxTimeoutMs = 1<<31 - 1

// validateWorkflowTimeout is TS resolveTimeoutSettings (workflow-agent-timeout.ts,
// f1de9cb61f): a timeout the timers cannot honor is an InvalidArgumentError
// naming the field, raised before the first model call. Step, first-chunk and
// chunk timeouts must be between 0 and 2^31-1 ms; the total may also be
// negative, a deadline that has already passed. The TS checks for NaN,
// Infinity and non-numbers have no Go twin: a time.Duration is always a
// number.
func validateWorkflowTimeout(t *ai.TimeoutConfig) error {
	if t == nil {
		return nil
	}
	for _, f := range []struct {
		name          string
		value         *time.Duration
		allowNegative bool
	}{
		{"timeout.totalMs", t.Total, true},
		{"timeout.stepMs", t.PerStep, false},
		{"timeout.firstChunkMs", t.FirstChunk, false},
		{"timeout.chunkMs", t.PerChunk, false},
	} {
		if f.value == nil {
			continue
		}
		ms := float64(*f.value) / float64(time.Millisecond)
		if ms <= workflowMaxTimeoutMs && (ms >= 0 || f.allowNegative) {
			continue
		}
		message := fmt.Sprintf("%s must be a number of milliseconds between 0 and %d", f.name, workflowMaxTimeoutMs)
		if f.allowNegative {
			message = fmt.Sprintf("%s must be a number of milliseconds of at most %d", f.name, workflowMaxTimeoutMs)
		}
		return &providererrors.InvalidArgumentError{Field: f.name, Value: ms, Message: message}
	}
	return nil
}

// workflowExpiredTotalContext returns a context that ended with the total
// timeout's TimeoutError when the call's total deadline has already passed (a
// total of zero or less, TS toDeadlineMs clamps it to 0), so the run ends as
// timed out before its first model call. It returns ctx and false otherwise.
func workflowExpiredTotalContext(ctx context.Context, t *ai.TimeoutConfig) (context.Context, bool) {
	if t == nil || t.Total == nil || *t.Total > 0 {
		return ctx, false
	}
	expired, cancel := t.CreateTimeoutContext(ctx, "total")
	// The context already ended with the timeout cause: cancel only releases
	// its timer and keeps the cause.
	cancel()
	return expired, true
}

// workflowCallTimeout is the timeout a call runs with: the call's, else the
// agent's, replaced by one a prepareCall hook returned.
func workflowCallTimeout(agent, call *ai.TimeoutConfig, prepared *LanguageModelCallOptions) *ai.TimeoutConfig {
	timeout := agent
	if call != nil {
		timeout = call
	}
	if prepared != nil && prepared.Timeout != nil {
		timeout = prepared.Timeout
	}
	return timeout
}
