package codemode

import (
	"context"
	"sync"
	"time"
)

// cpuBudget tracks the CPU time sandboxed code has used in one invocation
// (TS executionPolicy.cpuTimeoutMs, `run` limits.cpuTimeoutMs). The sandbox
// runs synchronously on one goroutine and a host tool call blocks that
// goroutine, so the CPU time is the wall time the sandbox spends running JS:
// the budget is paused for the whole of a host tool call, so time spent
// waiting on tools never counts.
type cpuBudget struct {
	limit time.Duration

	mu       sync.Mutex
	used     time.Duration
	since    time.Time
	running  bool
	expired  bool
	stopped  bool
	timer    *time.Timer
	onExpire func()
}

func newCPUBudget(limitMs int) *cpuBudget {
	return &cpuBudget{limit: time.Duration(limitMs) * time.Millisecond}
}

// start begins counting. onExpire runs once, on its own goroutine, when the
// budget is exhausted.
func (b *cpuBudget) start(onExpire func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onExpire = onExpire
	b.running = true
	b.since = time.Now()
	b.timer = time.AfterFunc(b.limit, b.fire)
}

func (b *cpuBudget) fire() {
	b.mu.Lock()
	if b.stopped || b.expired || !b.running {
		b.mu.Unlock()
		return
	}
	// A pause/resume racing the timer may have moved the deadline.
	if remaining := b.limit - (b.used + time.Since(b.since)); remaining > 0 {
		b.timer.Reset(remaining)
		b.mu.Unlock()
		return
	}
	b.expired = true
	onExpire := b.onExpire
	b.mu.Unlock()
	if onExpire != nil {
		onExpire()
	}
}

// pause stops counting (the sandbox is blocked on a host call).
func (b *cpuBudget) pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running || b.stopped {
		return
	}
	b.used += time.Since(b.since)
	b.running = false
	if b.timer != nil {
		b.timer.Stop()
	}
}

// resume continues counting once the host call returned.
func (b *cpuBudget) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running || b.stopped || b.expired || b.timer == nil {
		return
	}
	b.running = true
	b.since = time.Now()
	b.timer.Reset(max(b.limit-b.used, 0))
}

// stop releases the timer; the budget no longer fires.
func (b *cpuBudget) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	if b.timer != nil {
		b.timer.Stop()
	}
}

// exceeded reports whether the budget has run out, including time used since
// the last resume that the timer has not yet acted on.
func (b *cpuBudget) exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.expired {
		return true
	}
	used := b.used
	if b.running {
		used += time.Since(b.since)
	}
	return b.limit > 0 && used >= b.limit
}

func (b *cpuBudget) hasExpired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.expired
}

type cpuBudgetKey struct{}

// withCPUBudget attaches b to ctx so the tool bridge can pause it around host
// calls and runInSandbox can run it.
func withCPUBudget(ctx context.Context, b *cpuBudget) context.Context {
	return context.WithValue(ctx, cpuBudgetKey{}, b)
}

func cpuBudgetFrom(ctx context.Context) *cpuBudget {
	b, _ := ctx.Value(cpuBudgetKey{}).(*cpuBudget)
	return b
}
