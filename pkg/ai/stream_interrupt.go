package ai

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Steering interrupt in StreamText (TS e4039889ba). A step's model call can be
// cancelled while it has produced no output, and is made again in the same
// step with the steering message. This file holds the pieces of that redo:
// the watcher of one model call, the stream a cancelled open leaves behind
// and the state a redo restarts the step from.

// errStepInterrupted is what a chunk read returns when a steering interrupt
// arrived while it waited.
var errStepInterrupted = errors.New("model call interrupted by a steering message")

// stepInterrupt watches one model call of a step for a steering interrupt (TS
// interruptController + watchInterrupt). A nil *stepInterrupt watches nothing.
type stepInterrupt struct {
	fired   atomic.Bool
	firedCh chan struct{}
	once    sync.Once

	mu           sync.Mutex
	stop         func()
	windowClosed bool
	cancels      []context.CancelFunc
}

// newStepInterrupt starts watching for an interrupt of run. It returns nil
// for a call without steering.
func newStepInterrupt(run *steeringRun) *stepInterrupt {
	if run == nil {
		return nil
	}
	si := &stepInterrupt{firedCh: make(chan struct{})}
	stop := run.watchInterrupt(func() {
		si.fired.Store(true)
		si.once.Do(func() { close(si.firedCh) })
	})
	si.mu.Lock()
	si.stop = stop
	si.mu.Unlock()
	return si
}

// wrap returns ctx, cancelled as soon as the interrupt fires: the model call
// runs in it (TS modelCallAbortSignal merges the interrupt signal).
func (si *stepInterrupt) wrap(ctx context.Context) context.Context {
	if si == nil {
		return ctx
	}
	c, cancel := context.WithCancel(ctx)
	si.mu.Lock()
	si.cancels = append(si.cancels, cancel)
	si.mu.Unlock()
	go func() {
		select {
		case <-si.firedCh:
			cancel()
		case <-c.Done():
		}
	}()
	return c
}

// windowOpen reports whether the model call can still be interrupted.
func (si *stepInterrupt) windowOpen() bool {
	if si == nil {
		return false
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	return !si.windowClosed
}

// channel is the channel a chunk read selects on: nil once the call can no
// longer be interrupted.
func (si *stepInterrupt) channel() <-chan struct{} {
	if !si.windowOpen() {
		return nil
	}
	return si.firedCh
}

// closeWindow ends the interrupt window: the call produced output (TS
// interruptWindowClosed + stopWatchingInterrupt). A steer with interrupt
// then waits for the next step boundary.
func (si *stepInterrupt) closeWindow() {
	if si == nil {
		return
	}
	si.mu.Lock()
	si.windowClosed = true
	stop := si.stop
	si.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// release stops watching and frees the contexts made by wrap.
func (si *stepInterrupt) release() {
	if si == nil {
		return
	}
	si.closeWindow()
	si.mu.Lock()
	cancels := si.cancels
	si.cancels = nil
	si.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// interrupted reports whether the interrupt fired and still decides the call:
// a caller abort or a timeout wins over an interrupt (TS isInterrupted).
func (si *stepInterrupt) interrupted(stepCtx context.Context) bool {
	return si != nil && si.fired.Load() && stepCtx.Err() == nil
}

// closesInterruptWindow reports whether a chunk ends the interrupt window
// (TS: model-call-end, tool-input-start or an output chunk).
func closesInterruptWindow(chunk provider.StreamChunk) bool {
	return chunk.Type == provider.ChunkTypeFinish ||
		chunk.Type == provider.ChunkTypeToolInputStart ||
		isOutputChunkForTiming(chunk)
}

// stepInterruptedStream stands for a model call whose open was cancelled by a
// steering interrupt: the first read reports it, and the step is redone.
type stepInterruptedStream struct{}

func (stepInterruptedStream) Next() (*provider.StreamChunk, error) { return nil, errStepInterrupted }
func (stepInterruptedStream) Err() error                           { return errStepInterrupted }
func (stepInterruptedStream) Close() error                         { return nil }

// stepRedoStart is the state a step started with. A redo runs PrepareStep
// again with it, not with what PrepareStep returned for the cancelled call.
type stepRedoStart struct {
	system              string
	instructionMessages []types.Message
	runtimeCtx          interface{}
	toolsCtx            map[string]interface{}
}

// stepRedo makes the current step again after an interrupt (TS streamStep
// `redo`).
type stepRedo struct {
	// stepCtx and telemetryStepCtx are the step's own: the step timeout keeps
	// running and its span stays open.
	stepCtx          context.Context
	telemetryStepCtx context.Context
}
