package ai

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r59 integrator: TS executeToolsFromStream catches a tool context that fails
// its schema (validateToolContext throws inside executeToolCall) and enqueues
// an `error` part, so the onError callback sees it, unlike the failures TS
// throws inside its stream.
func TestR59StreamTextToolContextFailureReachesOnError(t *testing.T) {
	var executed atomic.Int32
	var mu sync.Mutex
	var got []error
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:        failureToolCallModel(types.ToolCall{ID: "c1", ToolName: "strict", Arguments: map[string]interface{}{}}),
		Prompt:       "go",
		Tools:        []types.Tool{strictContextTool("strict", &executed)},
		ToolsContext: map[string]interface{}{"strict": map[string]interface{}{}},
		StopWhen:     []StopCondition{StepCountIs(3)},
		OnError:      func(_ context.Context, err error) { mu.Lock(); got = append(got, err); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.ReadAll()
	if !isToolContextValidationError(res.Err()) {
		t.Fatalf("Err() = %v, want the tool context ValidationError", res.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !isToolContextValidationError(got[0]) {
		t.Fatalf("OnError calls = %v, want the tool context ValidationError once", got)
	}
	if executed.Load() != 0 {
		t.Fatalf("tool executed %d times", executed.Load())
	}
}
