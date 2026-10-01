package bridge

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
)

// ExperimentalUserMessageSubmitterOptions wires the submitter to a bridge
// transport (normally a *Channel).
type ExperimentalUserMessageSubmitterOptions struct {
	// Send transmits one `user-message` request.
	Send func(UserMessageCommand) error
	// OnResponse subscribes to `user-message-response` frames and returns an
	// unsubscribe function.
	OnResponse func(func(*UserMessageResponse)) (unsubscribe func())
	// OnReconnect subscribes to transport reconnects and returns an
	// unsubscribe function.
	OnReconnect func(func()) (unsubscribe func())
}

// ExperimentalUserMessageSubmitter steers an in-flight bridge turn with
// acknowledged user messages. Mirrors TS
// `experimental_createBridgeUserMessageSubmitter` (eace6fb).
type ExperimentalUserMessageSubmitter struct {
	opts                 ExperimentalUserMessageSubmitterOptions
	unsubscribeResponse  func()
	unsubscribeReconnect func()

	mu      sync.Mutex
	closed  bool
	pending map[string]*pendingUserMessage
	order   []string
}

type pendingUserMessage struct {
	request UserMessageCommand
	result  chan error
}

// Errors reported by the submitter.
var (
	ErrUserMessagesClosed  = errors.New("The bridge turn is no longer accepting user messages.")    //nolint:staticcheck // matches TS SDK's exact error text
	ErrUserMessageTurnDone = errors.New("The bridge turn ended before accepting the user message.") //nolint:staticcheck // matches TS SDK's exact error text
)

// NewExperimentalUserMessageSubmitter subscribes to responses and reconnects.
func NewExperimentalUserMessageSubmitter(opts ExperimentalUserMessageSubmitterOptions) *ExperimentalUserMessageSubmitter {
	s := &ExperimentalUserMessageSubmitter{opts: opts, pending: map[string]*pendingUserMessage{}}
	s.unsubscribeResponse = opts.OnResponse(s.handleResponse)
	s.unsubscribeReconnect = opts.OnReconnect(s.handleReconnect)
	return s
}

// NewChannelUserMessageSubmitter wires a submitter to a Channel: requests are
// sent through it, `user-message-response` frames resolve them and pending
// requests are re-sent with the same messageId after each reconnect.
func NewChannelUserMessageSubmitter(ch *Channel) *ExperimentalUserMessageSubmitter {
	return NewExperimentalUserMessageSubmitter(ExperimentalUserMessageSubmitterOptions{
		Send: func(cmd UserMessageCommand) error { return ch.Send(cmd) },
		OnResponse: func(fn func(*UserMessageResponse)) func() {
			return ch.On(TypeUserMessageResponse, func(ev Event) {
				if r, ok := ev.Message.(*UserMessageResponse); ok {
					fn(r)
				}
			})
		},
		OnReconnect: ch.OnReconnect,
	})
}

// Submit sends text as a new user message and blocks until the bridge
// accepts it (nil), rejects it, the submitter closes, or ctx ends.
func (s *ExperimentalUserMessageSubmitter) Submit(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrUserMessagesClosed
	}
	entry := &pendingUserMessage{
		request: UserMessageCommand{MessageID: newUUID(), Text: text},
		result:  make(chan error, 1),
	}
	s.pending[entry.request.MessageID] = entry
	s.order = append(s.order, entry.request.MessageID)
	s.mu.Unlock()

	s.send(entry.request)

	select {
	case err := <-entry.result:
		return err
	case <-ctx.Done():
		s.mu.Lock()
		s.remove(entry.request.MessageID)
		s.mu.Unlock()
		return context.Cause(ctx)
	}
}

// Close rejects pending and future messages with err (default
// ErrUserMessageTurnDone for pending ones) and unsubscribes.
func (s *ExperimentalUserMessageSubmitter) Close(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	entries := s.takeAll()
	s.mu.Unlock()

	s.unsubscribeResponse()
	s.unsubscribeReconnect()
	if err == nil {
		err = ErrUserMessageTurnDone
	}
	for _, e := range entries {
		e.result <- err
	}
}

func (s *ExperimentalUserMessageSubmitter) send(request UserMessageCommand) {
	if err := s.opts.Send(request); err != nil {
		s.mu.Lock()
		entry := s.remove(request.MessageID)
		s.mu.Unlock()
		if entry != nil {
			entry.result <- err
		}
	}
}

func (s *ExperimentalUserMessageSubmitter) handleResponse(r *UserMessageResponse) {
	s.mu.Lock()
	entry := s.remove(r.MessageID)
	s.mu.Unlock()
	if entry == nil {
		return
	}
	if r.Accepted {
		entry.result <- nil
		return
	}
	msg := "The runtime rejected the user message."
	if r.Error != nil {
		msg = r.Error.Message
	}
	entry.result <- errors.New(msg)
}

func (s *ExperimentalUserMessageSubmitter) handleReconnect() {
	s.mu.Lock()
	requests := make([]UserMessageCommand, 0, len(s.order))
	for _, id := range s.order {
		if e, ok := s.pending[id]; ok {
			requests = append(requests, e.request)
		}
	}
	s.mu.Unlock()
	for _, r := range requests {
		s.send(r)
	}
}

// remove deletes and returns a pending entry; s.mu must be held.
func (s *ExperimentalUserMessageSubmitter) remove(id string) *pendingUserMessage {
	entry, ok := s.pending[id]
	if !ok {
		return nil
	}
	delete(s.pending, id)
	for i, v := range s.order {
		if v == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return entry
}

// takeAll removes every pending entry; s.mu must be held.
func (s *ExperimentalUserMessageSubmitter) takeAll() []*pendingUserMessage {
	out := make([]*pendingUserMessage, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.pending[id])
	}
	s.pending = map[string]*pendingUserMessage{}
	s.order = nil
	return out
}

// newUUID returns a random RFC 4122 version 4 UUID (TS crypto.randomUUID).
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
