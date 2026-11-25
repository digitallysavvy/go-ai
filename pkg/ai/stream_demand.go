package ai

import (
	"context"
	"sync"
)

// streamDemand is the reader demand of a single-owner audio stream pipe
// (StreamTranscribe, StreamTranslate, StreamSpeech). TS pipes the model
// stream through a TransformStream whose readable side has a high-water mark
// of 0, so transform runs only while the output's reader has a read pending
// (or the result promises drain it): a part the output does not forward
// (stream-start, response-metadata, finish) is not processed before the
// reader asks for the next part. The pipe calls wait before each provider
// part while it holds no demand, and gives the demand back after each part
// it forwards.
type streamDemand struct {
	pull      chan struct{}
	drain     chan struct{}
	drainOnce sync.Once
}

func newStreamDemand() *streamDemand {
	return &streamDemand{pull: make(chan struct{}, 1), drain: make(chan struct{})}
}

// request records one pending read of the output (FullStream / AudioStream
// Next).
func (d *streamDemand) request() {
	select {
	case d.pull <- struct{}{}:
	default:
	}
}

// drainAll marks the output as drained by the result promises: from now on
// the pipe never waits.
func (d *streamDemand) drainAll() {
	d.drainOnce.Do(func() { close(d.drain) })
}

// wait blocks until there is demand, and reports false when ctx ends first.
// A pending read wins over an ended ctx, so a reader that is waiting still
// gets the part the provider returned.
func (d *streamDemand) wait(ctx context.Context) bool {
	select {
	case <-d.drain:
		return true
	case <-d.pull:
		return true
	default:
	}
	select {
	case <-d.drain:
		return true
	case <-d.pull:
		return true
	case <-ctx.Done():
		return false
	}
}
