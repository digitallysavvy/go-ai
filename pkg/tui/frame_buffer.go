package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	escape            = "\x1b"
	cursorHome        = escape + "[H"
	clearScreen       = escape + "[2J"
	clearLine         = escape + "[2K"
	synchronizedStart = escape + "[?2026h"
	synchronizedEnd   = escape + "[?2026l"
)

// TerminalFrameBuffer presents full terminal frames using ANSI escape sequences
// and diffs subsequent frames line-by-line, matching the TypeScript TUI buffer.
type TerminalFrameBuffer struct {
	writer                 io.Writer
	useSynchronizedUpdates bool
	previous               []string
	lastFrame              string
	externalWrite          bool
	mu                     sync.Mutex
}

type TerminalFrameBufferOptions struct {
	UseSynchronizedUpdates *bool
}

func NewTerminalFrameBuffer(writer io.Writer, options ...TerminalFrameBufferOptions) *TerminalFrameBuffer {
	useSync := true
	if len(options) > 0 && options[0].UseSynchronizedUpdates != nil {
		useSync = *options[0].UseSynchronizedUpdates
	}
	return &TerminalFrameBuffer{writer: writer, useSynchronizedUpdates: useSync}
}

func (b *TerminalFrameBuffer) Present(frame string) {
	if b == nil || b.writer == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	next := strings.Split(frame, "\n")
	b.lastFrame = frame
	update := ""
	if b.previous == nil || b.externalWrite {
		update = cursorHome + clearScreen + frame
	} else {
		update = diffFrameLines(b.previous, next)
	}
	b.previous = next
	b.externalWrite = false
	if update == "" {
		return
	}
	if b.useSynchronizedUpdates {
		update = synchronizedStart + update + synchronizedEnd
	}
	_, _ = io.WriteString(b.writer, update)
}

func (b *TerminalFrameBuffer) LastFrame() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastFrame
}

func (b *TerminalFrameBuffer) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.previous = nil
}

// MarkExternalWrite forces the next Present call to repaint the full frame.
// Node's TypeScript implementation detects this by wrapping output.write; Go's
// io.Writer cannot be safely monkey-patched, so callers can mark it explicitly.
func (b *TerminalFrameBuffer) MarkExternalWrite() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.externalWrite = true
}

func diffFrameLines(previous, next []string) string {
	var out strings.Builder
	lineCount := len(previous)
	if len(next) > lineCount {
		lineCount = len(next)
	}
	for i := 0; i < lineCount; i++ {
		line := ""
		if i < len(next) {
			line = next[i]
		}
		prev := ""
		if i < len(previous) {
			prev = previous[i]
		}
		if prev == line {
			continue
		}
		out.WriteString(fmt.Sprintf("%s[%d;1H%s%s", escape, i+1, clearLine, line))
	}
	return out.String()
}
