//go:build linux

package codemode

import (
	"runtime"
	"testing"
	"time"
)

func TestLinuxThreadMeterCountsCPUNotSleep(t *testing.T) {
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
	if r[0] > 30*time.Millisecond {
		t.Fatalf("sleeping counted as CPU: %v", r[0])
	}
	if r[1] < 5*time.Millisecond {
		t.Fatalf("busy loop not counted as CPU: %v", r[1])
	}
}
