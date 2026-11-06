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

// TS ToolLoopAgent runs generateText, whose start and step-start events carry
// the call id (the same one as the end event) and the call's tool settings.
// The legacy loop left them empty.
func TestR64GenerateAgentStartEventsCarryCallIDAndToolSettings(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	var mu sync.Mutex
	var start ai.OnStartEvent
	var stepStart ai.OnStepStartEvent
	var endCallID string
	choice := types.ToolChoice{Type: types.ToolChoiceNone}
	_, err := NewToolLoopAgent(AgentConfig{
		Model:       model,
		Tools:       []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
		ActiveTools: []string{"t"},
		ToolChoice:  choice,
		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			mu.Lock()
			start = e
			mu.Unlock()
		},
		OnStepStartEvent: func(_ context.Context, e ai.OnStepStartEvent) {
			mu.Lock()
			stepStart = e
			mu.Unlock()
		},
		OnFinishEvent: func(_ context.Context, e ai.OnFinishEvent) {
			mu.Lock()
			endCallID = e.CallID
			mu.Unlock()
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if endCallID == "" || start.CallID != endCallID || stepStart.CallID != endCallID {
		t.Fatalf("call ids: start %q, step start %q, end %q; want one non-empty id", start.CallID, stepStart.CallID, endCallID)
	}
	if start.OperationID != "ai.generateText" || start.MaxRetries != 2 {
		t.Fatalf("start event OperationID %q MaxRetries %d, want ai.generateText and 2", start.OperationID, start.MaxRetries)
	}
	if len(start.ActiveTools) != 1 || stepStart.ToolChoice != choice || len(stepStart.ActiveTools) != 1 {
		t.Fatalf("start ActiveTools %v, step ToolChoice %+v ActiveTools %v; want the configured settings", start.ActiveTools, stepStart.ToolChoice, stepStart.ActiveTools)
	}
}
