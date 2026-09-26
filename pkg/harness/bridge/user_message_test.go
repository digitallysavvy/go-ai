package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Port of harness/src/utils/bridge-user-message-submitter.test.ts.

type fakeSubmitterTransport struct {
	mu               sync.Mutex
	sent             []UserMessageCommand
	responseListener func(*UserMessageResponse)
	reconnectHandler func()
	unsubResponse    int
	unsubReconnect   int
}

func newFakeSubmitterTransport() *fakeSubmitterTransport { return &fakeSubmitterTransport{} }

func (f *fakeSubmitterTransport) opts() ExperimentalUserMessageSubmitterOptions {
	return ExperimentalUserMessageSubmitterOptions{
		Send: func(cmd UserMessageCommand) error {
			f.mu.Lock()
			f.sent = append(f.sent, cmd)
			f.mu.Unlock()
			return nil
		},
		OnResponse: func(fn func(*UserMessageResponse)) func() {
			f.mu.Lock()
			f.responseListener = fn
			f.mu.Unlock()
			return func() {
				f.mu.Lock()
				f.unsubResponse++
				f.mu.Unlock()
			}
		},
		OnReconnect: func(fn func()) func() {
			f.mu.Lock()
			f.reconnectHandler = fn
			f.mu.Unlock()
			return func() {
				f.mu.Lock()
				f.unsubReconnect++
				f.mu.Unlock()
			}
		},
	}
}

func (f *fakeSubmitterTransport) respond(resp *UserMessageResponse) {
	f.mu.Lock()
	fn := f.responseListener
	f.mu.Unlock()
	if fn != nil {
		fn(resp)
	}
}

func (f *fakeSubmitterTransport) reconnect() {
	f.mu.Lock()
	fn := f.reconnectHandler
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (f *fakeSubmitterTransport) Sent() []UserMessageCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]UserMessageCommand(nil), f.sent...)
}

// TS: "resolves only after the matching acceptance response"
func TestUserMessageSubmitterResolvesAfterAcceptance(t *testing.T) {
	f := newFakeSubmitterTransport()
	s := NewExperimentalUserMessageSubmitter(f.opts())

	done := make(chan error, 1)
	go func() { done <- s.Submit(context.Background(), "Change course.") }()

	// Give Submit a moment to send before asserting nothing settled yet.
	time.Sleep(20 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Submit settled before a response arrived: %v", err)
	default:
	}

	sent := f.Sent()
	if len(sent) != 1 || sent[0].Text != "Change course." {
		t.Fatalf("sent = %+v", sent)
	}
	if sent[0].MessageID == "" {
		t.Fatal("expected a generated messageId")
	}

	f.respond(&UserMessageResponse{MessageID: sent[0].MessageID, Accepted: true})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Submit returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Submit did not resolve after acceptance")
	}
}

// TS: "surfaces a runtime rejection"
func TestUserMessageSubmitterSurfacesRejection(t *testing.T) {
	f := newFakeSubmitterTransport()
	s := NewExperimentalUserMessageSubmitter(f.opts())

	done := make(chan error, 1)
	go func() { done <- s.Submit(context.Background(), "Change course.") }()
	waitFor(t, time.Second, func() bool { return len(f.Sent()) == 1 })

	f.respond(&UserMessageResponse{
		MessageID: f.Sent()[0].MessageID,
		Accepted:  false,
		Error:     &UserMessageResponseError{Message: "Turn already finished."},
	})

	select {
	case err := <-done:
		if err == nil || err.Error() != "Turn already finished." {
			t.Fatalf("err = %v, want %q", err, "Turn already finished.")
		}
	case <-time.After(time.Second):
		t.Fatal("Submit did not resolve after rejection")
	}
}

// TS: "retries pending messages with the same id after reconnect"
func TestUserMessageSubmitterRetriesAfterReconnect(t *testing.T) {
	f := newFakeSubmitterTransport()
	s := NewExperimentalUserMessageSubmitter(f.opts())
	t.Cleanup(func() { s.Close(nil) })

	go func() { _ = s.Submit(context.Background(), "Change course.") }()
	waitFor(t, time.Second, func() bool { return len(f.Sent()) == 1 })

	f.reconnect()
	waitFor(t, time.Second, func() bool { return len(f.Sent()) == 2 })

	sent := f.Sent()
	if sent[0] != sent[1] {
		t.Fatalf("expected the retried request to equal the original: %+v vs %+v", sent[0], sent[1])
	}
}

// TS: "rejects pending and future messages when closed"
func TestUserMessageSubmitterRejectsWhenClosed(t *testing.T) {
	f := newFakeSubmitterTransport()
	s := NewExperimentalUserMessageSubmitter(f.opts())

	done := make(chan error, 1)
	go func() { done <- s.Submit(context.Background(), "Change course.") }()
	waitFor(t, time.Second, func() bool { return len(f.Sent()) == 1 })

	s.Close(errors.New("Turn ended."))

	select {
	case err := <-done:
		if err == nil || err.Error() != "Turn ended." {
			t.Fatalf("err = %v, want %q", err, "Turn ended.")
		}
	case <-time.After(time.Second):
		t.Fatal("Submit did not resolve after Close")
	}

	err := s.Submit(context.Background(), "Again.")
	if err == nil || !isClosedSubmitterErr(err) {
		t.Fatalf("Submit after Close: err = %v, want ErrUserMessagesClosed", err)
	}

	f.mu.Lock()
	unsubR, unsubC := f.unsubResponse, f.unsubReconnect
	f.mu.Unlock()
	if unsubR != 1 || unsubC != 1 {
		t.Fatalf("unsubscribe counts = (%d, %d), want (1, 1)", unsubR, unsubC)
	}
}

func isClosedSubmitterErr(err error) bool { return errors.Is(err, ErrUserMessagesClosed) }
