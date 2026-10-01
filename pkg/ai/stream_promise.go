package ai

import "sync"

// streamPromise is a promise-like container resolved at most once, used by
// ExperimentalStreamTranscribe / ExperimentalStreamTranslate result accessors
// to mirror the TS SDK's DelayedPromise: resolve/reject after the first call
// are no-ops, and Wait blocks until a value or error is available.
type streamPromise[T any] struct {
	once sync.Once
	done chan struct{}
	val  T
	err  error
}

func newStreamPromise[T any]() *streamPromise[T] {
	return &streamPromise[T]{done: make(chan struct{})}
}

func (p *streamPromise[T]) resolve(v T) {
	p.once.Do(func() {
		p.val = v
		close(p.done)
	})
}

func (p *streamPromise[T]) reject(err error) {
	p.once.Do(func() {
		p.err = err
		close(p.done)
	})
}

func (p *streamPromise[T]) isPending() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// wait blocks until the promise is resolved or rejected.
func (p *streamPromise[T]) wait() (T, error) {
	<-p.done
	return p.val, p.err
}
