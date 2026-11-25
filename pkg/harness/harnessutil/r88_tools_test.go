package harnessutil

import (
	"testing"
	"time"
)

func TestR88toolsHostDisconnectTimeoutFlag(t *testing.T) {
	for d, want := range map[time.Duration]string{
		60 * time.Second:        " --host-disconnect-timeout-ms '60000'",
		1500 * time.Microsecond: " --host-disconnect-timeout-ms '1.5'",
		0:                       "",
		-time.Second:            "",
	} {
		if got := HostDisconnectTimeoutFlag(d); got != want {
			t.Errorf("HostDisconnectTimeoutFlag(%v) = %q, want %q", d, got, want)
		}
	}
}
