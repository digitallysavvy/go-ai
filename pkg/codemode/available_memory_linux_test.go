//go:build linux

package codemode

import (
	"math"
	"testing"
)

func TestLinuxAvailableMemoryParsing(t *testing.T) {
	if v, ok := leadingUint64("  123 kB\n"); !ok || v != 123 {
		t.Fatalf("leadingUint64 = %d, %v", v, ok)
	}
	if _, ok := leadingUint64("max\n"); ok {
		t.Fatal("max parsed as a number")
	}
	if v, ok := leadingUint64("99999999999999999999999"); !ok || v != math.MaxUint64 {
		t.Fatalf("overflow = %d, %v; want saturation", v, ok)
	}
	if p, ok := cgroup1MemoryPath("12:cpu,cpuacct:/a\n9:memory:/docker/abc\n"); !ok || p != "docker/abc" {
		t.Fatalf("cgroup1MemoryPath = %q, %v", p, ok)
	}
	if _, ok := cgroup1MemoryPath("0::/user.slice\n"); ok {
		t.Fatal("found a memory controller in a cgroup2 file")
	}
	if p := cgroup2Path("0::/user.slice/app.scope\n"); p != "user.slice/app.scope" {
		t.Fatalf("cgroup2Path = %q", p)
	}
	if b, ok := systemAvailableMemory(); !ok || b == 0 {
		t.Fatalf("systemAvailableMemory = %d, %v; want a reading", b, ok)
	}
}
