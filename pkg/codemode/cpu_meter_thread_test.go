//go:build linux || darwin

package codemode

import (
	"runtime"
	"testing"
	"time"
)

func TestThreadMeterCountsCPUNotSleep(t *testing.T) {
	done := make(chan [2]time.Duration)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		m := newSandboxMeter()
		a := m()
		time.Sleep(100 * time.Millisecond)
		slept := m() - a
		a = m()
		for x := time.Now(); time.Since(x) < 50*time.Millisecond; {
		}
		spun := m() - a
		done <- [2]time.Duration{slept, spun}
	}()
	r := <-done
	t.Logf("slept %v, spun %v", r[0], r[1])
	if r[0] > 30*time.Millisecond {
		t.Fatalf("sleeping counted as CPU: %v", r[0])
	}
	if r[1] < 5*time.Millisecond {
		t.Fatalf("busy loop not counted as CPU: %v", r[1])
	}
}

// The budget timer reads the meter from another goroutine (another thread):
// the reading must still be the sandbox thread's CPU, not the caller's.
func TestThreadMeterReadsPinnedThreadFromOtherGoroutine(t *testing.T) {
	meterc := make(chan func() time.Duration)
	release := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		meterc <- newSandboxMeter()
		<-release
	}()
	m := <-meterc
	defer close(release)
	a := m()
	// This goroutine burns CPU on its own thread while the sandbox thread
	// is parked: none of it may be counted.
	for x := time.Now(); time.Since(x) < 80*time.Millisecond; {
	}
	if d := m() - a; d > 20*time.Millisecond {
		t.Fatalf("another thread's CPU counted: %v", d)
	}
}
