//go:build unix

package harnessutil

import (
	"syscall"
	"time"
)

// processCPUTime is the CPU time (user + system) this test process has used.
// Unlike wall time it does not grow while other processes hold the CPUs.
func processCPUTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return time.Duration(time.Now().UnixNano())
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
