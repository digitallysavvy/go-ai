package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// DiagnosticEventType is the stable name of a telemetry diagnostic event.
type DiagnosticEventType string

const (
	DiagnosticEventOnStart                  DiagnosticEventType = "onStart"
	DiagnosticEventOnStepStart              DiagnosticEventType = "onStepStart"
	DiagnosticEventOnLanguageModelCallStart DiagnosticEventType = "onLanguageModelCallStart"
	DiagnosticEventOnLanguageModelCallEnd   DiagnosticEventType = "onLanguageModelCallEnd"
	DiagnosticEventOnToolExecutionStart     DiagnosticEventType = "onToolExecutionStart"
	DiagnosticEventOnToolExecutionEnd       DiagnosticEventType = "onToolExecutionEnd"
	DiagnosticEventOnChunk                  DiagnosticEventType = "onChunk"
	DiagnosticEventOnStepFinish             DiagnosticEventType = "onStepFinish"
	DiagnosticEventOnObjectStepStart        DiagnosticEventType = "onObjectStepStart"
	DiagnosticEventOnObjectStepFinish       DiagnosticEventType = "onObjectStepFinish"
	DiagnosticEventOnEmbedStart             DiagnosticEventType = "onEmbedStart"
	DiagnosticEventOnEmbedEnd               DiagnosticEventType = "onEmbedEnd"
	DiagnosticEventOnRerankStart            DiagnosticEventType = "onRerankStart"
	DiagnosticEventOnRerankEnd              DiagnosticEventType = "onRerankEnd"
	DiagnosticEventOnEnd                    DiagnosticEventType = "onEnd"
	DiagnosticEventOnError                  DiagnosticEventType = "onError"

	// Deprecated compatibility aliases.
	DiagnosticEventOnEmbedFinish  DiagnosticEventType = DiagnosticEventOnEmbedEnd
	DiagnosticEventOnRerankFinish DiagnosticEventType = DiagnosticEventOnRerankEnd
	DiagnosticEventOnFinish       DiagnosticEventType = DiagnosticEventOnEnd
)

// DiagnosticMessage is published to diagnostic subscribers for every telemetry
// lifecycle event.
type DiagnosticMessage struct {
	Type  DiagnosticEventType
	Event interface{}
}

// DiagnosticHandler receives diagnostic messages.
type DiagnosticHandler func(context.Context, DiagnosticMessage) error

// DiagnosticChannel is a concurrency-safe pub/sub channel for telemetry events.
type DiagnosticChannel struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]DiagnosticHandler
}

// NewDiagnosticChannel creates an empty diagnostic channel.
func NewDiagnosticChannel() *DiagnosticChannel {
	return &DiagnosticChannel{subscribers: make(map[uint64]DiagnosticHandler)}
}

// Subscribe registers handler and returns an unsubscribe function.
func (c *DiagnosticChannel) Subscribe(handler DiagnosticHandler) func() {
	if c == nil || handler == nil {
		return func() {}
	}
	id := atomic.AddUint64(&c.nextID, 1)
	c.mu.Lock()
	if c.subscribers == nil {
		c.subscribers = make(map[uint64]DiagnosticHandler)
	}
	c.subscribers[id] = handler
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subscribers, id)
			c.mu.Unlock()
		})
	}
}

// Publish notifies all current subscribers concurrently and returns any
// subscriber errors joined together. Panics from subscribers are captured as
// errors so one subscriber cannot break another.
func (c *DiagnosticChannel) Publish(ctx context.Context, msg DiagnosticMessage) error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	handlers := make([]DiagnosticHandler, 0, len(c.subscribers))
	for _, handler := range c.subscribers {
		handlers = append(handlers, handler)
	}
	c.mu.RUnlock()
	if len(handlers) == 0 {
		return nil
	}

	errs := make([]error, len(handlers))
	var wg sync.WaitGroup
	for i, handler := range handlers {
		wg.Add(1)
		go func(i int, handler DiagnosticHandler) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					errs[i] = fmt.Errorf("diagnostic subscriber panic: %v", recovered)
				}
			}()
			errs[i] = handler(ctx, msg)
		}(i, handler)
	}
	wg.Wait()

	var joined []error
	for _, err := range errs {
		if err != nil {
			joined = append(joined, err)
		}
	}
	return errors.Join(joined...)
}

var defaultDiagnosticChannel = NewDiagnosticChannel()

// DefaultDiagnosticChannel returns the process-wide telemetry diagnostic channel.
func DefaultDiagnosticChannel() *DiagnosticChannel {
	return defaultDiagnosticChannel
}

// SubscribeDiagnostic subscribes to the process-wide diagnostic channel.
func SubscribeDiagnostic(handler DiagnosticHandler) func() {
	return defaultDiagnosticChannel.Subscribe(handler)
}

// SubscribeDiagnosticTyped subscribes to messages of eventType and type-checks
// the event payload before invoking handler.
func SubscribeDiagnosticTyped[E any](eventType DiagnosticEventType, handler func(context.Context, E) error) func() {
	if handler == nil {
		return func() {}
	}
	return SubscribeDiagnostic(func(ctx context.Context, msg DiagnosticMessage) error {
		if msg.Type != eventType {
			return nil
		}
		event, ok := msg.Event.(E)
		if !ok {
			return fmt.Errorf("diagnostic event %s has payload %T", msg.Type, msg.Event)
		}
		return handler(ctx, event)
	})
}

// PublishDiagnostic publishes to the process-wide diagnostic channel and
// intentionally swallows subscriber failures to match telemetry callback
// behavior.
func PublishDiagnostic(ctx context.Context, eventType DiagnosticEventType, event interface{}) {
	_ = defaultDiagnosticChannel.Publish(ctx, DiagnosticMessage{Type: eventType, Event: event})
}
