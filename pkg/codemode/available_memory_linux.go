//go:build linux

package codemode

import (
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// systemAvailableMemory ports libuv's uv_get_available_memory on Linux
// (src/unix/linux.c, libuv 1.52), which backs Node's process.availableMemory()
// and so TS run's default worker cap: the cgroup memory limit minus the
// cgroup's current usage when a limit below total memory is set, otherwise
// MemAvailable from /proc/meminfo.
func systemAvailableMemory() (uint64, bool) {
	cgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0, true
	}
	buf := string(cgroup)
	v2 := strings.HasPrefix(buf, "0::/")

	constrained := cgroupConstrainedMemory(buf, v2)
	if constrained == 0 {
		return freeMemory(), true
	}
	if constrained > totalMemory() {
		return freeMemory(), true
	}
	var current uint64
	if v2 {
		current = readCgroupUint64("/sys/fs/cgroup/" + cgroup2Path(buf) + "/memory.current")
	} else {
		current = cgroup1CurrentMemory(buf)
	}
	// Memory usage can be higher than the limit for short bursts.
	if constrained < current {
		return 0, true
	}
	return constrained - current, true
}

func freeMemory() uint64 {
	if rc := readMeminfo("MemAvailable:"); rc != 0 {
		return rc
	}
	var info syscall.Sysinfo_t
	if syscall.Sysinfo(&info) == nil {
		return uint64(info.Freeram) * uint64(info.Unit)
	}
	return 0
}

func totalMemory() uint64 {
	if rc := readMeminfo("MemTotal:"); rc != 0 {
		return rc
	}
	var info syscall.Sysinfo_t
	if syscall.Sysinfo(&info) == nil {
		return uint64(info.Totalram) * uint64(info.Unit)
	}
	return 0
}

// readMeminfo is libuv's uv__read_proc_meminfo: the kB value after what.
func readMeminfo(what string) uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	s := string(data)
	i := strings.Index(s, what)
	if i < 0 {
		return 0
	}
	v, _ := leadingUint64(s[i+len(what):])
	return v * 1024
}

// readCgroupUint64 is libuv's uv__read_uint64: the leading decimal value,
// math.MaxUint64 for cgroup2's "max", and 0 when unreadable.
func readCgroupUint64(filename string) uint64 {
	data, err := os.ReadFile(filename)
	if err != nil {
		return 0
	}
	if v, ok := leadingUint64(string(data)); ok {
		return v
	}
	if string(data) == "max\n" {
		return math.MaxUint64
	}
	return 0
}

// leadingUint64 reads a decimal number after optional whitespace, as sscanf
// "%" PRIu64 does (saturating on overflow like strtoull).
func leadingUint64(s string) (uint64, bool) {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[:n], 10, 64)
	if err != nil {
		return math.MaxUint64, true
	}
	return v, true
}

func cgroup2Path(buf string) string {
	p := buf[len("0::/"):]
	if i := strings.IndexByte(p, '\n'); i >= 0 {
		p = p[:i]
	}
	return p
}

// cgroup1MemoryPath is libuv's uv__cgroup1_find_memory_controller: the mount
// path of the memory controller without its leading slash.
func cgroup1MemoryPath(buf string) (string, bool) {
	for _, line := range strings.Split(buf, "\n") {
		i := strings.IndexByte(line, ':')
		if i < 0 || !strings.HasPrefix(line[i:], ":memory:") {
			continue
		}
		return strings.TrimPrefix(line[i+len(":memory:"):], "/"), true
	}
	return "", false
}

func cgroupConstrainedMemory(buf string, v2 bool) uint64 {
	var high, maxBytes uint64
	if v2 {
		dir := "/sys/fs/cgroup/" + cgroup2Path(buf)
		maxBytes = readCgroupUint64(dir + "/memory.max")
		high = readCgroupUint64(dir + "/memory.high")
	} else {
		high, maxBytes = cgroup1MemoryLimits(buf)
	}
	if high == 0 || maxBytes == 0 {
		return 0
	}
	return min(high, maxBytes)
}

func cgroup1MemoryLimits(buf string) (high, maxBytes uint64) {
	found := false
	if p, ok := cgroup1MemoryPath(buf); ok {
		high = readCgroupUint64("/sys/fs/cgroup/memory/" + p + "/memory.soft_limit_in_bytes")
		maxBytes = readCgroupUint64("/sys/fs/cgroup/memory/" + p + "/memory.limit_in_bytes")
		found = high != 0 && maxBytes != 0
	}
	if !found {
		high = readCgroupUint64("/sys/fs/cgroup/memory/memory.soft_limit_in_bytes")
		maxBytes = readCgroupUint64("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	}
	// cgroup1's "no limit" is LONG_MAX rounded down to the page size.
	cgroup1Max := uint64(math.MaxInt) &^ uint64(os.Getpagesize()-1)
	if high == cgroup1Max {
		high = math.MaxUint64
	}
	if maxBytes == cgroup1Max {
		maxBytes = math.MaxUint64
	}
	return high, maxBytes
}

func cgroup1CurrentMemory(buf string) uint64 {
	if p, ok := cgroup1MemoryPath(buf); ok {
		if current := readCgroupUint64("/sys/fs/cgroup/memory/" + p + "/memory.usage_in_bytes"); current != 0 {
			return current
		}
	}
	return readCgroupUint64("/sys/fs/cgroup/memory/memory.usage_in_bytes")
}
