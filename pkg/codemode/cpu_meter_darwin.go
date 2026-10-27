//go:build darwin

package codemode

import (
	"os"
	"syscall"
	"time"
	"unsafe"
	_ "unsafe" // go:linkname
)

// procThreadInfo is the kernel's struct proc_threadinfo
// (<sys/proc_info.h>); the times are nanoseconds.
type procThreadInfo struct {
	UserTime   uint64
	SystemTime uint64
	_          [8]int32 // cpu usage, policy, run state, flags, sleep time, priorities
	_          [64]byte // thread name
}

const procPIDThreadID64Info = 15 // PROC_PIDTHREADID64INFO

// newSandboxMeter returns a reading of the CPU time used by the OS thread the
// caller is running on. The caller (runInSandbox's goroutine) is pinned to
// that thread with runtime.LockOSThread. darwin's CLOCK_THREAD_CPUTIME_ID only
// reads the calling thread, so the meter names the thread by its 64-bit id
// (pthread_threadid_np) and reads it with libproc's proc_pidinfo
// PROC_PIDTHREADID64INFO, which works from any goroutine (the budget timer).
// Both are libSystem calls made the way golang.org/x/sys/unix makes them, with
// no cgo. If either is unavailable the meter falls back to wall time.
func newSandboxMeter() func() time.Duration {
	var tid uint64
	if r, _, _ := syscall_syscall(libc_pthread_threadid_np_trampoline_addr, 0, uintptr(unsafe.Pointer(&tid)), 0); r != 0 {
		return newWallMeter()
	}
	pid := os.Getpid()
	if _, ok := readThreadCPU(pid, tid); !ok {
		return newWallMeter()
	}
	var last time.Duration
	return func() time.Duration {
		if d, ok := readThreadCPU(pid, tid); ok {
			last = d
		}
		return last // thread gone: no more CPU is being used
	}
}

func readThreadCPU(pid int, tid uint64) (time.Duration, bool) {
	var info procThreadInfo
	n, _, _ := syscall_syscall6(libc_proc_pidinfo_trampoline_addr, uintptr(pid), procPIDThreadID64Info,
		uintptr(tid), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	if int32(n) != int32(unsafe.Sizeof(info)) {
		return 0, false
	}
	return time.Duration(info.UserTime + info.SystemTime), true
}

// The libSystem call path golang.org/x/sys/unix uses on darwin
// (syscall_darwin_libSystem.go): the runtime's syscall/syscall6 switch to
// the system stack and call a libc function through an assembly trampoline
// (cpu_meter_darwin_*.s).

//go:linkname syscall_syscall syscall.syscall
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

var libc_pthread_threadid_np_trampoline_addr uintptr

//go:cgo_import_dynamic libc_pthread_threadid_np pthread_threadid_np "/usr/lib/libSystem.B.dylib"

var libc_proc_pidinfo_trampoline_addr uintptr

//go:cgo_import_dynamic libc_proc_pidinfo proc_pidinfo "/usr/lib/libSystem.B.dylib"
