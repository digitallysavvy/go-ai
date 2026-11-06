//go:build darwin || linux || freebsd || openbsd || netbsd

package tui

import (
	"context"
	"io"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"
)

func enableRawTerminalMode(input io.Reader) func() {
	file, ok := input.(*os.File)
	if !ok {
		return nil
	}
	fd := int(file.Fd())
	original, err := getTerminalAttributes(fd)
	if err != nil {
		return nil
	}

	raw := rawTermios(original)
	if err := setTerminalAttributes(fd, &raw); err != nil {
		return nil
	}

	return func() {
		_ = setTerminalAttributes(fd, original)
	}
}

// rawTermios mirrors libuv's UV_TTY_MODE_RAW (what Node's setRawMode(true)
// applies): output post-processing stays enabled with ONLCR, so the "\n"
// separators of a full-frame repaint still return the carriage. Clearing
// OPOST would make every line of the first frame start where the previous
// one ended.
func rawTermios(original *unix.Termios) unix.Termios {
	raw := *original
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag |= unix.OPOST | unix.ONLCR
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	return raw
}

func isTerminalInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	_, err := getTerminalAttributes(int(file.Fd()))
	return err == nil
}

func terminalOutputSize(output io.Writer) (int, int) {
	file, ok := output.(*os.File)
	if !ok {
		return 0, 0
	}
	size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return int(size.Col), int(size.Row)
}

func startTerminalResizeWatcher(output io.Writer, repaint func()) func() {
	if columns, rows := terminalOutputSize(output); columns <= 0 || rows <= 0 {
		return nil
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				repaint()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

func waitForTerminalInputByte(ctx context.Context, input io.Reader, timeout time.Duration) error {
	file, ok := input.(*os.File)
	if !ok {
		return nil
	}
	fd := int(file.Fd())
	if _, err := getTerminalAttributes(fd); err != nil {
		return nil
	}

	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pollTimeout := 50
		if timeout >= 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return context.DeadlineExceeded
			}
			pollTimeout = int(remaining.Milliseconds())
			if pollTimeout < 1 {
				pollTimeout = 1
			}
			if pollTimeout > 50 {
				pollTimeout = 50
			}
		}

		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, pollTimeout)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		if n > 0 && fds[0].Revents&unix.POLLIN != 0 {
			return nil
		}
	}
}
