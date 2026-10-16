//go:build darwin || linux || freebsd || openbsd || netbsd

package tui

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestRawTermiosKeepsOutputPostProcessing(t *testing.T) {
	orig := &unix.Termios{}
	orig.Iflag = unix.ICRNL | unix.IXON
	orig.Lflag = unix.ECHO | unix.ICANON | unix.ISIG
	raw := rawTermios(orig)
	if raw.Oflag&unix.OPOST == 0 || raw.Oflag&unix.ONLCR == 0 {
		t.Fatalf("OPOST/ONLCR must stay enabled so \\n returns the carriage, oflag=%#x", raw.Oflag)
	}
	if raw.Lflag&(unix.ECHO|unix.ICANON|unix.ISIG) != 0 || raw.Iflag&(unix.ICRNL|unix.IXON) != 0 {
		t.Fatalf("input must be raw: %#x %#x", raw.Lflag, raw.Iflag)
	}
	if raw.Cc[unix.VMIN] != 1 || raw.Cc[unix.VTIME] != 0 {
		t.Fatal("VMIN/VTIME")
	}
}
