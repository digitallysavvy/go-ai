package codemode

import (
	"sync/atomic"
	"testing"
	"time"
)

// fakeMeter is a controllable CPU clock.
type fakeMeter struct{ ns atomic.Int64 }

func (f *fakeMeter) read() time.Duration { return time.Duration(f.ns.Load()) }
func (f *fakeMeter) add(d time.Duration) { f.ns.Add(int64(d)) }

func TestCPUBudgetCountsMeterNotWallTime(t *testing.T) {
	m := &fakeMeter{}
	b := newCPUBudget(100)
	b.meter = m.read
	b.start(func() {})
	defer b.stop()

	// Wall time passes but the meter (real CPU) does not move: not exceeded.
	time.Sleep(30 * time.Millisecond)
	if b.exceeded() {
		t.Fatal("budget exceeded without CPU use")
	}
	m.add(99 * time.Millisecond)
	if b.exceeded() {
		t.Fatal("exceeded at 99ms of 100ms")
	}
	m.add(2 * time.Millisecond)
	if !b.exceeded() {
		t.Fatal("not exceeded at 101ms of 100ms")
	}
}

func TestCPUBudgetPauseExcludesMeterAdvance(t *testing.T) {
	m := &fakeMeter{}
	b := newCPUBudget(100)
	b.meter = m.read
	b.start(func() {})
	defer b.stop()

	m.add(60 * time.Millisecond)
	b.pause()
	m.add(500 * time.Millisecond) // host call: must not count
	b.resume()
	if b.exceeded() {
		t.Fatal("paused time counted")
	}
	m.add(41 * time.Millisecond)
	if !b.exceeded() {
		t.Fatal("budget should be exceeded at 101ms of CPU")
	}
}

func TestCPUBudgetTimerRearmsWhenCPULagsWall(t *testing.T) {
	m := &fakeMeter{}
	b := newCPUBudget(40)
	b.meter = m.read
	fired := make(chan struct{}, 1)
	b.start(func() { fired <- struct{}{} })
	defer b.stop()

	// The timer's first deadline (40ms wall) passes with no CPU used: it must
	// re-arm rather than expire.
	select {
	case <-fired:
		t.Fatal("expired without CPU use")
	case <-time.After(150 * time.Millisecond):
	}
	if b.hasExpired() {
		t.Fatal("expired without CPU use")
	}
	m.add(41 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("budget did not expire once CPU was used")
	}
}
