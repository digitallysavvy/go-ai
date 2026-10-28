package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS filterActiveTools: activeTools [] leaves no tool active (only an unset
// activeTools keeps them all), so the legacy loop sends the model no tools
// and a call to a configured tool is a NoSuchToolError listing none.
func TestR53LegacyLoopEmptyActiveToolsSendsNoTools(t *testing.T) {
	for _, tc := range []struct {
		name      string
		active    []string
		wantTools int
	}{
		{"unset", nil, 1},
		{"empty", []string{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var sent []int
			model := &testutil.MockLanguageModel{
				ProviderName: "p", ModelName: "m",
				DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
					mu.Lock()
					sent = append(sent, len(o.Tools))
					mu.Unlock()
					return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
				},
			}
			a := NewToolLoopAgent(AgentConfig{
				Model:       model,
				Tools:       []types.Tool{{Name: "weather", Parameters: map[string]interface{}{"type": "object"}}},
				ActiveTools: tc.active,
				StopWhen:    []ai.StopCondition{ai.StepCountIs(1)},
			})
			if _, err := a.Execute(context.Background(), "hi"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(sent) != 1 || sent[0] != tc.wantTools {
				t.Fatalf("tools sent = %v, want %d", sent, tc.wantTools)
			}
		})
	}
}
