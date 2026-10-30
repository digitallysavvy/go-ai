package ai

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS executeToolsFromStream catches a tool context that fails its schema
// (validateToolContext throws inside executeToolCall) and enqueues an `error`
// part: onChunk and onError see it, the step still finishes (onStepEnd, not
// onStepError), and the call without a result starts no next step, so the run
// ends with finish reason tool-calls and its result promises resolve (r70:
// the run no longer fails).
func TestR59StreamTextToolContextFailureReachesOnError(t *testing.T) {
	var executed atomic.Int32
	var mu sync.Mutex
	var events []string
	var got []error
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:        failureToolCallModel(types.ToolCall{ID: "c1", ToolName: "strict", Arguments: map[string]interface{}{}}),
		Prompt:       "go",
		Tools:        []types.Tool{strictContextTool("strict", &executed)},
		ToolsContext: map[string]interface{}{"strict": map[string]interface{}{}},
		StopWhen:     []StopCondition{StepCountIs(3)},
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			if c.Type == provider.ChunkTypeError {
				events = append(events, "chunk:error")
			}
		},
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, err)
			events = append(events, "onError")
		},
		OnStepError: func(context.Context, OnStepErrorEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "onStepError")
		},
		OnStepEnd: func(context.Context, types.StepResult, interface{}) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "onStepEnd")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	s := res.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, string(c.Type))
	}
	if res.Err() != nil {
		t.Fatalf("Err() = %v, want nil (the error part does not fail the run)", res.Err())
	}
	if res.FinishReason() != types.FinishReasonToolCalls || len(res.Steps()) != 1 {
		t.Fatalf("finish reason %q, %d steps; want tool-calls and 1 step", res.FinishReason(), len(res.Steps()))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !isToolContextValidationError(got[0]) {
		t.Fatalf("OnError calls = %v, want the tool context ValidationError once", got)
	}
	if want := "chunk:error onError onStepEnd"; joinStrings(events) != want {
		t.Fatalf("events = %q, want %q", joinStrings(events), want)
	}
	if executed.Load() != 0 {
		t.Fatalf("tool executed %d times", executed.Load())
	}
	wantParts := []string{"start", "start-step", "tool-call", "error", "finish-step", "finish"}
	if joinStrings(parts) != joinStrings(wantParts) {
		t.Fatalf("full stream = %v, want %v", parts, wantParts)
	}
}

func joinStrings(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}
