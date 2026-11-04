package harnessutil

import "sync"

// Subscriptions collects a turn's bridge listener unsubscribes. A channel
// replays a buffered terminal event synchronously from On, so a turn can
// settle while its handlers are still being registered: Add after
// UnsubscribeAll runs the unsubscribe at once, and the list is guarded
// because settling runs on the channel's dispatch goroutine. The zero value
// is ready to use.
type Subscriptions struct {
	mu           sync.Mutex
	list         []func()
	unsubscribed bool
}

// Add records unsubscribe, or calls it now when UnsubscribeAll already ran.
func (s *Subscriptions) Add(unsubscribe func()) {
	s.mu.Lock()
	if s.unsubscribed {
		s.mu.Unlock()
		unsubscribe()
		return
	}
	s.list = append(s.list, unsubscribe)
	s.mu.Unlock()
}

// UnsubscribeAll calls every recorded unsubscribe once; later Adds
// unsubscribe immediately.
func (s *Subscriptions) UnsubscribeAll() {
	s.mu.Lock()
	list := s.list
	s.list, s.unsubscribed = nil, true
	s.mu.Unlock()
	for _, u := range list {
		u()
	}
}
