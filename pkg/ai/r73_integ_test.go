package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r73 stream->gen CR-1: GenerateText's step-end event carries the step's
// steering messages (TS GenerateTextStepEndEvent = StepResult, which has
// experimental_steeringMessages).
func TestR73Integ_GenerateTextStepEndEventCarriesSteeringMessages(t *testing.T) {
	steering := NewSteering()
	steering.Steer("first")
	var mu sync.Mutex
	var events []OnStepFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:                r72genSeqModel(t, r72genText("hello")),
		Prompt:               "hello",
		ExperimentalSteering: steering,
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(res.Steps) != 1 || len(res.Steps[0].ExperimentalSteeringMessages) != 1 {
		t.Fatalf("step steering messages = %+v", res.Steps)
	}
	if len(events) != 1 || !reflect.DeepEqual(events[0].ExperimentalSteeringMessages, res.Steps[0].ExperimentalSteeringMessages) {
		t.Fatalf("step-end event steering messages = %+v", events)
	}
}

// r73 gen CR-2: an element stream that exceeds maxItems fails with TS
// getArrayLengthValidationError: a TypeValidationError holding the partial
// array.
func TestR73Integ_ElementStreamMaxItemsIsTypeValidationError(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(append(textChunks(`{"elements":[`, `"e1",`, `"e2",`, `"e3"]}`), finishChunk())), nil
		},
	}
	maxItems := 1
	output := ArrayOutput[string](ArrayOutputOptions[string]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"}),
		MaxItems:      &maxItems,
	})
	var streamErr error
	got := collectElements(ElementStream[string](mustStream(t, model, output), ElementStreamOptions[string]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"}),
		MaxItems:      &maxItems,
		OnError:       func(e error) { streamErr = e },
	}))
	if !reflect.DeepEqual(got, []string{"e1"}) {
		t.Fatalf("elements = %#v", got)
	}
	var tve *providererrors.TypeValidationError
	if !errors.As(streamErr, &tve) {
		t.Fatalf("error = %T %v, want *TypeValidationError", streamErr, streamErr)
	}
	if !reflect.DeepEqual(tve.Value, []string{"e1", "e2", "e3"}) {
		t.Errorf("value = %#v", tve.Value)
	}
	if want := "Type validation failed: Value: [\"e1\",\"e2\",\"e3\"].\nError message: elements array must contain at most 1 items"; tve.Error() != want {
		t.Errorf("message = %q, want %q", tve.Error(), want)
	}
}

// Coordinator ruling r73 (gen open item): parallel tool calls fire their
// start callbacks in call order, as TS executeTools does (generate-text.test.ts
// 4642), and their executions still overlap. call-1's start callback is slow,
// so without the ordering call-2/call-3 would start first; call-1's Execute
// waits until call-3's Execute runs, which deadlocks (test timeout) if the
// executions were serialized.
func TestR73Integ_ParallelToolStartCallbacksInCallOrder(t *testing.T) {
	call3Running := make(chan struct{})
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			switch opts.ToolCallID {
			case "call-1":
				select {
				case <-call3Running:
				case <-time.After(5 * time.Second):
					return nil, errors.New("call-3 never ran while call-1 was executing")
				}
			case "call-3":
				close(call3Running)
			}
			return "ok", nil
		},
	}
	for _, concurrency := range []*int{nil, r73integIntPtr(3)} {
		call3Running = make(chan struct{})
		var mu sync.Mutex
		var starts []string
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r72genSeqModel(t,
				r72genCalls(
					types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{}},
					types.ToolCall{ID: "call-3", ToolName: "tool1", Arguments: map[string]interface{}{}}),
				r72genText("done")),
			Prompt:              "go",
			Tools:               []types.Tool{tool},
			StopWhen:            []StopCondition{StepCountIs(2)},
			ToolCallConcurrency: concurrency,
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					time.Sleep(30 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				starts = append(starts, e.ToolCall.ID)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := strings.Join(starts, ",")
		mu.Unlock()
		if got != "call-1,call-2,call-3" {
			t.Errorf("concurrency=%v: start order = %s, want call-1,call-2,call-3", concurrency, got)
		}
		if len(res.Steps) != 2 || len(res.Steps[0].ToolResults) != 3 {
			t.Errorf("concurrency=%v: steps = %d, first step results = %+v", concurrency, len(res.Steps), res.Steps[0].ToolResults)
		}
	}
}

func r73integIntPtr(v int) *int { return &v }

// r73 integrator survivor test (gen fix 3): each finish part replaces the
// call's provider metadata (TS stream-object.ts assigns
// `providerMetadata = chunk.providerMetadata` in every 'finish'), so a later
// finish part without metadata leaves none.
func TestR73Integ_StreamObjectLastFinishPartMetadata(t *testing.T) {
	first := finishChunk()
	first.ProviderMetadata = json.RawMessage(`{"testProvider":{"key":"value"}}`)
	chunks := append(textChunks(`{ "content": "x" }`), first, finishChunk())
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(chunks...), Prompt: "prompt", Schema: contentSchema(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMetadata != nil {
		t.Fatalf("ProviderMetadata = %#v, want nil (the last finish part has none)", res.ProviderMetadata)
	}
}
