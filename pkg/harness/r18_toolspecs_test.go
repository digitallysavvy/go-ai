package harness

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tool specs are sent to the adapter in a stable order: Go map iteration is
// random, and order-sensitive consumers (Codex turn fingerprint, ACP
// lossy-rerun catalog check) must not see a different catalog every turn.
func TestAgentToolSpecsHaveStableOrder(t *testing.T) {
	userTools := map[string]types.Tool{}
	for _, name := range []string{"zeta", "alpha", "mu", "beta", "omega", "gamma", "delta", "kappa"} {
		userTools[name] = types.Tool{Name: name, Description: name}
	}
	for run := 0; run < 6; run++ {
		mock := newMockHarness(mockHarnessOptions{
			script: func(submit func(string, interface{})) []StreamPart {
				return []StreamPart{
					&StreamStartPart{},
					&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
					&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
				}
			},
		})
		a, session := newTestAgent(t, mock, userTools, Callbacks{}, ToolApprovalConfiguration{})
		if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, spec := range mock.turnSettings[0].Tools {
			names = append(names, spec.Name)
		}
		want := []string{"alpha", "beta", "delta", "gamma", "kappa", "mu", "omega", "zeta"}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("run %d: tool spec order = %v, want %v", run, names, want)
		}
	}
}
