//go:build !linux

package codemode

import "time"

// newSandboxMeter falls back to wall time on platforms where another
// goroutine cannot read the sandbox thread's CPU clock without cgo (darwin's
// CLOCK_THREAD_CPUTIME_ID only reports the calling thread). Host-call time is
// still excluded by the budget's pause/resume.
func newSandboxMeter() func() time.Duration { return newWallMeter() }
