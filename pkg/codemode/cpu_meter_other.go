//go:build !linux && !darwin

package codemode

import "time"

// newSandboxMeter falls back to wall time on platforms where the budget timer
// cannot read the sandbox thread's CPU time without cgo (cpu_meter_linux.go
// and cpu_meter_darwin.go read it). Host-call time is still excluded by the
// budget's pause/resume.
func newSandboxMeter() func() time.Duration { return newWallMeter() }
