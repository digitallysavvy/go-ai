package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
