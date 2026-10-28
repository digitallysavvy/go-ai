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
				StopWhen:    []ai.StopCondition{ai.IsStepCount(1)},
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

// TS parseToolCall hands the repair function the step's messages
// (stepMessages); the instructions are passed separately, never prepended
// as system messages.
func TestR53LegacyLoopRepairGetsStepMessages(t *testing.T) {
	var got []types.Message
	a := NewToolLoopAgent(AgentConfig{
		Model: r52ToolCallModel(types.ToolCall{ID: "c1", ToolName: "missing", RawArguments: `{}`}),
		Tools: []types.Tool{{Name: "weather", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		InstructionMessages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}}},
		StopWhen:            []ai.StopCondition{ai.IsStepCount(1)},
		RepairToolCall: func(_ context.Context, o ai.ToolCallRepairOptions) (*types.ToolCall, error) {
			got = o.Messages
			return nil, nil
		},
	})
	if _, err := a.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != types.RoleUser {
		t.Fatalf("repair messages = %+v, want only the user message", got)
	}
}
