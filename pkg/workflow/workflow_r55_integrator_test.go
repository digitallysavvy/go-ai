package workflow

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Every value TS's iterator keeps across steps (currentGenerationSettings,
// currentToolChoice, currentActiveTools, currentRuntimeContext,
// currentToolsContext) reaches the next step's PrepareStep hook after a
// step-0 override, in both modes.
func TestWorkflowPrepareStepEveryPersistedValueReachesNextStep(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			fp, pp, seed, retries := 0.3, 0.4, 7, 1
			reasoning := types.ReasoningHigh
			set := func(o *LanguageModelCallOptions) {
				o.FrequencyPenalty, o.PresencePenalty, o.StopSequences, o.Seed = &fp, &pp, []string{"STOP"}, &seed
				o.MaxRetries, o.Headers, o.Reasoning = &retries, map[string]string{"x-h": "1"}, &reasoning
				o.ProviderOptions = map[string]interface{}{"p": map[string]interface{}{"k": "v"}}
				o.ToolChoice, o.ActiveTools = types.ToolChoice{Type: types.ToolChoiceType("auto")}, []string{"t"}
				o.RuntimeContext, o.ToolsContext = "rc", map[string]interface{}{"t": "tc"}
			}
			var want LanguageModelCallOptions
			set(&want)
			describe := func(o LanguageModelCallOptions) string {
				deref := func(p interface{}) interface{} {
					v := reflect.ValueOf(p)
					if v.IsNil() {
						return nil
					}
					return v.Elem().Interface()
				}
				return fmt.Sprint(deref(o.FrequencyPenalty), deref(o.PresencePenalty), o.StopSequences, deref(o.Seed),
					deref(o.MaxRetries), o.Headers, deref(o.Reasoning), o.ProviderOptions,
					o.ToolChoice, o.ActiveTools, o.RuntimeContext, o.ToolsContext)
			}
			var mu sync.Mutex
			var step1 []string
			m := &scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: m,
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					if o.StepNumber == 0 {
						set(&o)
					} else {
						mu.Lock()
						step1 = append(step1, describe(o))
						mu.Unlock()
					}
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
				if err != nil {
					t.Fatal(err)
				}
				_ = res.ConsumeStream()
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go"}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(step1) == 0 {
				t.Fatal("PrepareStep never ran for step 1")
			}
			if step1[0] != describe(want) {
				t.Fatalf("step 1 hook input = %s\nwant %s", step1[0], describe(want))
			}
		})
	}
}
