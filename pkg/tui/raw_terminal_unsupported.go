//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package tui

import (
	"context"
	"io"
	"time"
)

func enableRawTerminalMode(_ io.Reader) func() {
	return nil
}

func isTerminalInput(_ io.Reader) bool {
	return false
}

func terminalOutputSize(_ io.Writer) (int, int) {
	return 0, 0
}

func startTerminalResizeWatcher(_ io.Writer, _ func()) func() {
	return nil
}

func waitForTerminalInputByte(ctx context.Context, _ io.Reader, timeout time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if timeout == 0 {
		return context.DeadlineExceeded
	}
	return nil
}
