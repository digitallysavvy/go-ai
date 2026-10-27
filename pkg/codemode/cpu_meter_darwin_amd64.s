// Trampolines for the libSystem calls in cpu_meter_darwin.go, in the form
// golang.org/x/sys/unix uses (zsyscall_darwin_*.s).

#include "textflag.h"

TEXT libc_pthread_threadid_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_threadid_np(SB)
GLOBL	·libc_pthread_threadid_np_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_threadid_np_trampoline_addr(SB)/8, $libc_pthread_threadid_np_trampoline<>(SB)

TEXT libc_proc_pidinfo_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_pidinfo(SB)
GLOBL	·libc_proc_pidinfo_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_pidinfo_trampoline_addr(SB)/8, $libc_proc_pidinfo_trampoline<>(SB)
