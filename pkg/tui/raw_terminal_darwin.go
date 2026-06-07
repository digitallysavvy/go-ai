//go:build darwin || freebsd || openbsd || netbsd

package tui

import "golang.org/x/sys/unix"

func getTerminalAttributes(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TIOCGETA)
}

func setTerminalAttributes(fd int, termios *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
}
