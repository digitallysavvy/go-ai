//go:build !linux && !darwin && !windows

package codemode

// systemAvailableMemory is not implemented on this platform without cgo
// (available_memory_linux.go, available_memory_darwin.go and
// available_memory_windows.go port libuv's uv_get_available_memory). With no
// reading, the default worker cap is TS run's ceiling of 32 and no memory is
// reserved; Node would use the platform's free memory here.
func systemAvailableMemory() (uint64, bool) { return 0, false }
