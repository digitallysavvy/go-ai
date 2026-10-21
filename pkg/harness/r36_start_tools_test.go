package harness

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS onStart / onStepStart events carry `tools` (the merged tool set) and
// `activeTools` (the names the runtime may invoke this turn).
func TestRunPrompt_StartEventsCarryToolsAndActiveTools(t *testing.T) {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "done", nil
	}
	tools := map[string]types.Tool{
		"b": {Name: "b", Description: "inactive", Parameters: map[string]any{"type": "object"}, Execute: exec},
		"a": {Name: "a", Description: "active", Parameters: map[string]any{"type": "object"}, Execute: exec},
	}
	active := map[string]types.Tool{"a": tools["a"]}
	session := &slowToolTurnSession{toolName: "a"}

	var mu sync.Mutex
	var start ai.OnStartEvent
	var stepStart ai.OnStepStartEvent
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: active,
		SandboxSession: testSandbox(), Model: "harness-model",
		Callbacks: Callbacks{
			OnStart:     func(_ context.Context, e ai.OnStartEvent) { mu.Lock(); start = e; mu.Unlock() },
			OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) { mu.Lock(); stepStart = e; mu.Unlock() },
		},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done

	mu.Lock()
	defer mu.Unlock()
	names := func(ts []types.Tool) []string {
		var n []string
		for _, tool := range ts {
			n = append(n, tool.Name)
		}
		return n
	}
	if got := names(start.Tools); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("OnStart tools = %v, want [a b]", got)
	}
	if !reflect.DeepEqual(start.ActiveTools, []string{"a"}) {
		t.Fatalf("OnStart activeTools = %v, want [a]", start.ActiveTools)
	}
	if got := names(stepStart.Tools); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("OnStepStart tools = %v, want [a b]", got)
	}
	if !reflect.DeepEqual(stepStart.ActiveTools, []string{"a"}) {
		t.Fatalf("OnStepStart activeTools = %v, want [a]", stepStart.ActiveTools)
	}
}
