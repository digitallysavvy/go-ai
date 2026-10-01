package workflow

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestStreamTextIteratorSequenceAndClose(t *testing.T) {
	it := NewStreamTextIterator([]types.StepResult{{StepNumber: 1, Text: "a"}, {StepNumber: 2, Text: "b"}})
	step, err := it.Next(context.Background())
	if err != nil || step.StepNumber != 1 {
		t.Fatalf("first Next() got step=%+v err=%v", step, err)
	}
	step, err = it.Next(context.Background())
	if err != nil || step.StepNumber != 2 {
		t.Fatalf("second Next() got step=%+v err=%v", step, err)
	}
	_, err = it.Next(context.Background())
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if err := it.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	_, err = it.Next(context.Background())
	if err != io.EOF {
		t.Fatalf("expected io.EOF after close, got %v", err)
	}
}

func TestStreamTextIteratorFromResult(t *testing.T) {
	it := NewStreamTextIteratorFromResult(&WorkflowResult{
		AgentResult: &agent.AgentResult{
			Steps: []types.StepResult{{StepNumber: 9, Text: "x"}},
		},
	})
	step, err := it.Next(context.Background())
	if err != nil || step.StepNumber != 9 {
		t.Fatalf("unexpected step=%+v err=%v", step, err)
	}
}

func TestStreamTextIteratorFromChannel(t *testing.T) {
	ch := make(chan types.StepResult, 1)
	ch <- types.StepResult{StepNumber: 3, Text: "live"}
	close(ch)

	it := NewStreamTextIteratorFromChannel(ch)
	step, err := it.Next(context.Background())
	if err != nil || step.StepNumber != 3 {
		t.Fatalf("unexpected step=%+v err=%v", step, err)
	}
	_, err = it.Next(context.Background())
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
