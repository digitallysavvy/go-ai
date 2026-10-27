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
	cfgTimeout, callTimeout := 1000*time.Millisecond, 5*time.Millisecond
	cfg := AgentConfig{Timeout: &ai.TimeoutConfig{ToolMs: &cfgTimeout}}
	call := &ai.TimeoutConfig{ToolMs: &callTimeout}
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

// The per-call timeout also wins over a timeout PrepareCall returns: TS
// prepareCall never sees the call's timeout, and generate/stream use
// `timeout ?? preparedCall.timeout`.
func TestPerCallTimeoutWinsOverPrepareCallTimeout(t *testing.T) {
	prepared, callTimeoutDur := time.Hour, time.Millisecond
	call := &ai.TimeoutConfig{ToolMs: &callTimeoutDur}
	agent := NewToolLoopAgent(AgentConfig{
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Timeout = &ai.TimeoutConfig{ToolMs: &prepared}
			return c
		},
	})
	if got := agent.prepareGenerateCallConfig(context.Background(), "p", nil, call); got.Timeout != call {
		t.Fatalf("timeout = %+v, want the per-call timeout", got.Timeout)
	}
	if got := agent.prepareGenerateCallConfig(context.Background(), "p", nil, nil); got.Timeout == nil || got.Timeout.ToolMs == nil || *got.Timeout.ToolMs != prepared {
		t.Fatalf("timeout = %+v, want PrepareCall's timeout without a per-call one", got.Timeout)
	}
}
