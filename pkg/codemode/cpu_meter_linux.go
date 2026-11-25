//go:build linux

package codemode

import (
	"time"

	"golang.org/x/sys/unix"
)

// newSandboxMeter returns a reading of the CPU time used by the OS thread the
// caller is running on. The caller (runInSandbox's goroutine) is pinned to
// that thread with runtime.LockOSThread, and the reading stays valid from
// other goroutines (the budget timer) because the per-thread clock id is
// derived from the thread id instead of using CLOCK_THREAD_CPUTIME_ID, which
// only means "the calling thread". If the clock is unavailable the meter
// falls back to wall time.
func newSandboxMeter() func() time.Duration {
	tid := unix.Gettid()
	id := threadCPUClockID(tid)
	var ts unix.Timespec
	if err := unix.ClockGettime(id, &ts); err != nil {
		return newWallMeter()
	}
	var last time.Duration
	return func() time.Duration {
		var t unix.Timespec
		if err := unix.ClockGettime(id, &t); err != nil {
			return last // thread gone: no more CPU is being used
		}
		last = time.Duration(t.Nano())
		return last
	}
}

// threadCPUClockID mirrors the kernel's MAKE_THREAD_CPUCLOCK(tid, CPUCLOCK_SCHED).
func threadCPUClockID(tid int) int32 {
	return int32((^uint32(tid))<<3) | 2 | 4
}
