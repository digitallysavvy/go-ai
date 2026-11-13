//go:build darwin

package codemode

import (
	"os"
	"unsafe"
)

// hostVMInfo is HOST_VM_INFO and hostVMInfoCount is HOST_VM_INFO_COUNT, the
// number of natural_t fields in vm_statistics_data_t (<mach/vm_statistics.h>).
const (
	hostVMInfo      = 2
	hostVMInfoCount = 15
)

// vmStatistics is vm_statistics_data_t.
type vmStatistics struct {
	FreeCount        uint32
	ActiveCount      uint32
	InactiveCount    uint32
	WireCount        uint32
	ZeroFillCount    uint32
	Reactivations    uint32
	Pageins          uint32
	Pageouts         uint32
	Faults           uint32
	CowFaults        uint32
	Lookups          uint32
	Hits             uint32
	PurgeableCount   uint32
	Purges           uint32
	SpeculativeCount uint32
}

// systemAvailableMemory ports libuv's uv_get_available_memory on darwin
// (src/unix/darwin.c, libuv 1.52), which backs Node's process.availableMemory()
// and so TS run's default worker cap: (free + inactive + purgeable pages) ×
// page size from host_statistics(HOST_VM_INFO), 0 on failure. The libSystem
// calls are made through the trampolines in cpu_meter_darwin_*.s, with no cgo.
func systemAvailableMemory() (uint64, bool) {
	host, _, _ := syscall_syscall(libc_mach_host_self_trampoline_addr, 0, 0, 0)
	var info vmStatistics
	count := uint32(hostVMInfoCount)
	r, _, _ := syscall_syscall6(libc_host_statistics_trampoline_addr, host, hostVMInfo,
		uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&count)), 0, 0)
	if r != 0 {
		return 0, true
	}
	pages := uint64(info.FreeCount) + uint64(info.InactiveCount) + uint64(info.PurgeableCount)
	return pages * uint64(os.Getpagesize()), true
}

var libc_mach_host_self_trampoline_addr uintptr

//go:cgo_import_dynamic libc_mach_host_self mach_host_self "/usr/lib/libSystem.B.dylib"

var libc_host_statistics_trampoline_addr uintptr

//go:cgo_import_dynamic libc_host_statistics host_statistics "/usr/lib/libSystem.B.dylib"
