package workflow

import (
	"context"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// StreamTextIterator provides lazy iteration over workflow steps.
type StreamTextIterator struct {
	steps  []types.StepResult
	ch     <-chan types.StepResult
	idx    int
	closed bool
}

// NewStreamTextIterator creates an iterator over step results.
func NewStreamTextIterator(steps []types.StepResult) *StreamTextIterator {
	copied := make([]types.StepResult, len(steps))
	copy(copied, steps)
	return &StreamTextIterator{steps: copied}
}

// NewStreamTextIteratorFromResult creates an iterator from a workflow result.
func NewStreamTextIteratorFromResult(result *WorkflowResult) *StreamTextIterator {
	if result == nil || result.AgentResult == nil {
		return NewStreamTextIterator(nil)
	}
	return NewStreamTextIterator(result.Steps)
}

// NewStreamTextIteratorFromChannel creates a live iterator over step results.
func NewStreamTextIteratorFromChannel(ch <-chan types.StepResult) *StreamTextIterator {
	return &StreamTextIterator{ch: ch}
}

// Next returns the next step result, or io.EOF when exhausted.
func (it *StreamTextIterator) Next(ctx context.Context) (*types.StepResult, error) {
	if it == nil || it.closed {
		return nil, io.EOF
	}
	if it.ch != nil {
		if ctx == nil {
			step, ok := <-it.ch
			if !ok {
				return nil, io.EOF
			}
			return &step, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case step, ok := <-it.ch:
			if !ok {
				return nil, io.EOF
			}
			return &step, nil
		}
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if it.idx >= len(it.steps) {
		return nil, io.EOF
	}
	step := it.steps[it.idx]
	it.idx++
	return &step, nil
}

// Close marks iterator as closed.
func (it *StreamTextIterator) Close() error {
	if it == nil {
		return fmt.Errorf("workflow: nil iterator")
	}
	it.closed = true
	return nil
}
