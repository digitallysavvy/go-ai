package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS ToolLoopAgent.generate/stream use `timeout ?? preparedCall.timeout`:
// a per-call timeout overrides the agent's settings.
func TestPerCallTimeoutOverridesConfig(t *testing.T) {
	cfgMs, callMs := 1000*time.Millisecond, 5*time.Millisecond
	cfg := AgentConfig{Timeout: &ai.TimeoutConfig{ToolMs: &cfgMs}}
	call := &ai.TimeoutConfig{ToolMs: &callMs}
	got := cfg.withGenerateOptions(AgentGenerateOptions{Timeout: call})
	if got.Timeout != call {
		t.Fatalf("per-call timeout not applied: %+v", got.Timeout)
	}
	if kept := cfg.withGenerateOptions(AgentGenerateOptions{}); kept.Timeout != cfg.Timeout {
		t.Fatal("config timeout must be kept when no per-call timeout")
	}
	pc := (&ToolLoopAgent{config: got}).prepareStepCallConfig(context.Background(), 0, "p", nil, nil, types.Usage{}, nil)
	if pc.Timeout != call {
		t.Fatal("prepared call config lost per-call timeout")
	}
}
