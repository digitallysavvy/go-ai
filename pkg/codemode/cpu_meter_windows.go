//go:build windows

package codemode

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GetThreadTimes is not wrapped by golang.org/x/sys/windows; it is loaded
// lazily from kernel32 the way that package loads its own procs (no cgo).
var procGetThreadTimes = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetThreadTimes")

// threadMeter holds a real handle to the sandbox thread. The handle is
// closed by a cleanup once the meter is unreachable; holding it keeps the
// thread id from being reused by another thread while the meter is in use.
type threadMeter struct {
	h    windows.Handle
	last time.Duration
}

// newSandboxMeter returns a reading of the CPU time used by the OS thread the
// caller is running on. The caller (runInSandbox's goroutine) is pinned to
// that thread with runtime.LockOSThread. GetCurrentThread is a pseudo-handle
// meaning "the calling thread", so the meter opens a real handle to the thread
// by id and reads it with GetThreadTimes, which works from any goroutine (the
// budget timer). If the handle or the call is unavailable the meter falls back
// to wall time.
func newSandboxMeter() func() time.Duration {
	if procGetThreadTimes.Find() != nil {
		return newWallMeter()
	}
	h, err := windows.OpenThread(windows.THREAD_QUERY_LIMITED_INFORMATION, false, windows.GetCurrentThreadId())
	if err != nil {
		return newWallMeter()
	}
	d, ok := readThreadCPU(h)
	if !ok {
		_ = windows.CloseHandle(h)
		return newWallMeter()
	}
	m := &threadMeter{h: h, last: d}
	runtime.AddCleanup(m, func(h windows.Handle) { _ = windows.CloseHandle(h) }, h)
	return m.read
}

func (m *threadMeter) read() time.Duration {
	if d, ok := readThreadCPU(m.h); ok {
		m.last = d
	}
	return m.last // thread gone: no more CPU is being used
}

// readThreadCPU returns the thread's kernel plus user time. FILETIME
// durations are in 100 ns units.
func readThreadCPU(h windows.Handle) (time.Duration, bool) {
	var creation, exit, kernel, user windows.Filetime
	r, _, _ := syscall.SyscallN(procGetThreadTimes.Addr(), uintptr(h),
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, false
	}
	return time.Duration((filetimeTicks(kernel) + filetimeTicks(user)) * 100), true
}

func filetimeTicks(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
