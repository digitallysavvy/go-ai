package ai

import (
	"context"
	"encoding/json"
	"sync"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Experimental steering (TS experimental_createSteering, #23813).
//
// A Steering sends user messages to a running generation. Create it before the
// call, pass it as ExperimentalSteering to GenerateText, StreamText or the
// ToolLoopAgent, and call Steer while the call runs. A message is added at the
// next step boundary, after the current step and its tool calls are done;
// running tools are never interrupted. A message that is waiting when the model
// finishes its answer keeps the loop going for another step unless a stop
// condition is met.
//
// Queue (TS steering.queue(), #24209) adds a message once the model has
// finished its current task, that is when it gives a final answer without
// tool calls; the loop then continues with it unless a stop condition is met.
// Queued messages are delivered one per task, in order, after any waiting
// steering messages, and never after a response cut off by the length limit
// or a content filter.

// SteeringNotDeliveredReason says why a steering message was not delivered.
type SteeringNotDeliveredReason string

const (
	// SteeringFinished: the run ended before the message could be delivered.
	SteeringFinished SteeringNotDeliveredReason = "finished"
	// SteeringRemovedByPrepareStep: PrepareStep returned messages without it.
	SteeringRemovedByPrepareStep SteeringNotDeliveredReason = "removed-by-prepare-step"
	// SteeringStopCondition: a stop condition ended the run.
	SteeringStopCondition SteeringNotDeliveredReason = "stop-condition"
	// SteeringWaitingForToolResults: the run ended to wait for tool results,
	// for example a tool approval or a tool without an Execute function.
	SteeringWaitingForToolResults SteeringNotDeliveredReason = "waiting-for-tool-results"
	// SteeringAborted: the run was aborted.
	SteeringAborted SteeringNotDeliveredReason = "aborted"
	// SteeringError: the run failed.
	SteeringError SteeringNotDeliveredReason = "error"
)

// SteeringOutcome is the outcome of a steering message.
type SteeringOutcome struct {
	// Delivered is true when the message was added to a step of the run.
	Delivered bool
	// ID is the id of the steering message.
	ID string
	// Message is the user message that was (or was not) added to the run.
	Message types.Message
	// StepNumber is the 0-indexed step whose model call received the message.
	// Only set when Delivered.
	StepNumber int
	// Reason says why the message was not delivered. Empty when Delivered.
	Reason SteeringNotDeliveredReason
}

// SteerHandle is the pending outcome of a steering message. Steer never fails:
// the outcome is always one of delivered or not-delivered.
type SteerHandle struct {
	done    chan struct{}
	outcome SteeringOutcome
}

// Done is closed once the outcome is known.
func (h *SteerHandle) Done() <-chan struct{} { return h.done }

// Outcome blocks until the outcome is known and returns it.
func (h *SteerHandle) Outcome() SteeringOutcome {
	<-h.done
	return h.outcome
}

// Wait blocks until the outcome is known or ctx ends.
func (h *SteerHandle) Wait(ctx context.Context) (SteeringOutcome, error) {
	select {
	case <-h.done:
		return h.outcome, nil
	case <-ctx.Done():
		return SteeringOutcome{}, ctx.Err()
	}
}

func (h *SteerHandle) resolve(o SteeringOutcome) {
	h.outcome = o
	close(h.done)
}

// SteerOption configures Steer.
type SteerOption func(*steerConfig)

type steerConfig struct{ id string }

// WithSteerID sets the id of the message. Sending a message again with the
// same id returns the handle of the first message, so retries are safe.
func WithSteerID(id string) SteerOption {
	return func(c *steerConfig) { c.id = id }
}

type pendingSteer struct {
	id      string
	message types.Message
	handle  *SteerHandle
}

type steeringState int

const (
	steeringIdle steeringState = iota
	steeringRunning
	steeringEnded
)

// Steering sends user messages to a running generation. It can be used for
// one generation only. Create it with NewSteering.
type Steering struct {
	mu       sync.Mutex
	state    steeringState
	pending  []pendingSteer
	queued   []pendingSteer
	taken    []pendingSteer
	outcomes map[string]*SteerHandle
}

// NewSteering creates a Steering (TS experimental_createSteering). Experimental.
func NewSteering() *Steering {
	return &Steering{outcomes: map[string]*SteerHandle{}}
}

// Steer adds a user message with the given text to the run at the next step
// boundary. Messages sent before the run starts are delivered in the first
// step. A message sent after the run ended resolves as not delivered
// (SteeringFinished) right away.
func (s *Steering) Steer(text string, opts ...SteerOption) *SteerHandle {
	h, _ := s.SteerMessage(types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}, opts...)
	return h
}

// SteerMessage is Steer for a full user message. It returns an
// InvalidArgumentError for any other role.
func (s *Steering) SteerMessage(message types.Message, opts ...SteerOption) (*SteerHandle, error) {
	return s.add(message, false, opts)
}

// Queue adds a user message with the given text to the run once the model has
// finished its current task (a final answer without tool calls). The run then
// continues with the message unless a stop condition is met. Queued messages
// are delivered one per task, in order, after any waiting steering messages;
// messages queued before the run wait until the prompt is answered. Ids are
// shared with Steer: a message with an id that was already sent returns the
// handle of the first message. Mirrors TS steering.queue().
func (s *Steering) Queue(text string, opts ...SteerOption) *SteerHandle {
	h, _ := s.QueueMessage(types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}, opts...)
	return h
}

// QueueMessage is Queue for a full user message. It returns an
// InvalidArgumentError for any other role.
func (s *Steering) QueueMessage(message types.Message, opts ...SteerOption) (*SteerHandle, error) {
	return s.add(message, true, opts)
}

// add registers a steered (queued=false) or queued message (TS
// DefaultSteering.add).
func (s *Steering) add(message types.Message, queued bool, opts []SteerOption) (*SteerHandle, error) {
	var cfg steerConfig
	for _, o := range opts {
		o(&cfg)
	}
	id := cfg.id
	if id == "" {
		id = "steer-" + GenerateID()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.outcomes[id]; ok {
		return existing, nil
	}
	if message.Role != types.RoleUser {
		return nil, &providererrors.InvalidArgumentError{Field: "message", Message: "Steering messages must be user messages."}
	}
	h := &SteerHandle{done: make(chan struct{})}
	s.outcomes[id] = h
	if s.state == steeringEnded {
		h.resolve(SteeringOutcome{ID: id, Message: message, Reason: SteeringFinished})
		return h, nil
	}
	p := pendingSteer{id: id, message: message, handle: h}
	if queued {
		s.queued = append(s.queued, p)
	} else {
		s.pending = append(s.pending, p)
	}
	return h, nil
}

// steeringRun is the run side of a Steering.
type steeringRun struct{ s *Steering }

// startSteeringRun marks the steering as used by a generation. It returns nil
// for a nil Steering.
func startSteeringRun(s *Steering) (*steeringRun, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != steeringIdle {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "ExperimentalSteering",
			Message: "The steering object was already used by a generation. Create a new one with NewSteering() for each call.",
		}
	}
	s.state = steeringRunning
	return &steeringRun{s: s}, nil
}

func (r *steeringRun) hasPendingMessages() bool {
	if r == nil {
		return false
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.state == steeringRunning && len(r.s.pending) > 0
}

// promoteQueuedMessage moves the oldest queued message to the waiting
// messages for the next step. It returns false when nothing is queued, the
// run is not running, or r is nil (TS steering.ts 213-221).
func (r *steeringRun) promoteQueuedMessage() bool {
	if r == nil {
		return false
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.state != steeringRunning || len(r.s.queued) == 0 {
		return false
	}
	next := r.s.queued[0]
	r.s.queued = r.s.queued[1:]
	r.s.pending = append(r.s.pending, next)
	return true
}

// takeMessages takes the waiting messages for the next step; they are resolved
// by resolveTakenMessages once the step's messages are known.
func (r *steeringRun) takeMessages() []types.Message {
	if r == nil {
		return nil
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.state != steeringRunning {
		return nil
	}
	r.s.taken = append(r.s.taken, r.s.pending...)
	r.s.pending = nil
	out := make([]types.Message, 0, len(r.s.taken))
	for _, p := range r.s.taken {
		out = append(out, p.message)
	}
	return out
}

// steeringDelivered is a delivered steering message with its id (TS
// resolveTakenMessages result element { id, message }, steering.ts 127-131).
type steeringDelivered struct {
	ID      string
	Message types.Message
}

// resolveTakenMessages resolves the taken messages as delivered when they are
// part of sentMessages, and returns the delivered messages with their ids
// (TS SteeringRun.resolveTakenMessages, steering.ts 121-131 and 229-252;
// #24405 added the ids so streamText can stream a steering-message part per
// delivered message).
func (r *steeringRun) resolveTakenMessages(stepNumber int, sentMessages []types.Message) []steeringDelivered {
	if r == nil {
		return nil
	}
	r.s.mu.Lock()
	taken := r.s.taken
	r.s.taken = nil
	r.s.mu.Unlock()

	var delivered []steeringDelivered
	for _, p := range taken {
		if steerIsSent(p.message, sentMessages) {
			delivered = append(delivered, steeringDelivered{ID: p.id, Message: p.message})
			p.handle.resolve(SteeringOutcome{Delivered: true, ID: p.id, Message: p.message, StepNumber: stepNumber})
		} else {
			p.handle.resolve(SteeringOutcome{ID: p.id, Message: p.message, Reason: SteeringRemovedByPrepareStep})
		}
	}
	return delivered
}

// steeringDeliveredMessages returns the messages of delivered (TS
// `.map(({ message }) => message)`, generate-text.ts 1123-1128 and
// stream-text.ts 2650-2652). It returns nil for an empty input, so
// ExperimentalSteeringMessages stays nil when nothing was delivered.
func steeringDeliveredMessages(delivered []steeringDelivered) []types.Message {
	if len(delivered) == 0 {
		return nil
	}
	out := make([]types.Message, 0, len(delivered))
	for _, d := range delivered {
		out = append(out, d.Message)
	}
	return out
}

// end resolves every waiting message as not delivered and ends the run.
func (r *steeringRun) end(reason SteeringNotDeliveredReason) {
	if r == nil {
		return
	}
	r.s.mu.Lock()
	if r.s.state != steeringRunning {
		r.s.mu.Unlock()
		return
	}
	notDelivered := append(append(append([]pendingSteer(nil), r.s.taken...), r.s.pending...), r.s.queued...)
	r.s.taken, r.s.pending, r.s.queued = nil, nil, nil
	r.s.state = steeringEnded
	r.s.mu.Unlock()
	for _, p := range notDelivered {
		p.handle.resolve(SteeringOutcome{ID: p.id, Message: p.message, Reason: reason})
	}
}

// steerIsSent reports whether message is part of sentMessages. PrepareStep can
// map messages to new values (for example to add provider options), so a
// message also counts as sent when a user message has the same content.
func steerIsSent(message types.Message, sentMessages []types.Message) bool {
	content, _ := json.Marshal(message.Content)
	for _, sent := range sentMessages {
		if sent.Role != types.RoleUser {
			continue
		}
		if sentContent, _ := json.Marshal(sent.Content); string(sentContent) == string(content) {
			return true
		}
	}
	return false
}

// steeringEndReasonFor returns the reason a run ended because of err.
func steeringEndReasonFor(ctx context.Context, err error) SteeringNotDeliveredReason {
	if err != nil && isAbortErr(ctx, err) {
		return SteeringAborted
	}
	return SteeringError
}
