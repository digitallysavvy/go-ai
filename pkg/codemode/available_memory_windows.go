//go:build windows

package codemode

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// GlobalMemoryStatusEx is not wrapped by golang.org/x/sys/windows; it is
// loaded lazily from kernel32 like GetThreadTimes (cpu_meter_windows.go).
var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// systemAvailableMemory ports libuv's uv_get_available_memory on Windows
// (src/win/util.c), which backs Node's process.availableMemory() and so TS
// run's default worker cap: GlobalMemoryStatusEx's ullAvailPhys, 0 on failure.
func systemAvailableMemory() (uint64, bool) {
	if procGlobalMemoryStatusEx.Find() != nil {
		return 0, true
	}
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return 0, true
	}
	return status.AvailPhys, true
}
