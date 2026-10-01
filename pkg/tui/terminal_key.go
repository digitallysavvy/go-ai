package tui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"time"
)

// TerminalKeyType is a decoded terminal key kind.
type TerminalKeyType string

const (
	TerminalKeyCharacter TerminalKeyType = "character"
	TerminalKeyBackspace TerminalKeyType = "backspace"
	TerminalKeyEnter     TerminalKeyType = "enter"
	TerminalKeyUp        TerminalKeyType = "up"
	TerminalKeyDown      TerminalKeyType = "down"
	TerminalKeyPageUp    TerminalKeyType = "page-up"
	TerminalKeyPageDown  TerminalKeyType = "page-down"
	TerminalKeyCtrlL     TerminalKeyType = "ctrl-l"
	TerminalKeyCtrlC     TerminalKeyType = "ctrl-c"
	TerminalKeyEscape    TerminalKeyType = "escape"
	TerminalKeyIgnore    TerminalKeyType = "ignore"
)

// TerminalKey is the Go equivalent of the TypeScript TerminalKey union.
type TerminalKey struct {
	Type  TerminalKeyType
	Value string
}

// TerminalKeyReader can be implemented by tests or terminal adapters that
// already decode raw key events.
type TerminalKeyReader interface {
	ReadTerminalKey(ctx context.Context) (TerminalKey, error)
}

// ParseTerminalKey decodes the same terminal control sequences as the
// TypeScript parseKey helper.
func ParseTerminalKey(chunk []byte) TerminalKey {
	value := string(chunk)
	switch value {
	case "\u000c":
		return TerminalKey{Type: TerminalKeyCtrlL}
	case "\u0003":
		return TerminalKey{Type: TerminalKeyCtrlC}
	case "\x1B":
		return TerminalKey{Type: TerminalKeyEscape}
	case "\r", "\n":
		return TerminalKey{Type: TerminalKeyEnter}
	case "\u007f", "\b":
		return TerminalKey{Type: TerminalKeyBackspace}
	case "\x1B[A":
		return TerminalKey{Type: TerminalKeyUp}
	case "\x1B[B":
		return TerminalKey{Type: TerminalKeyDown}
	case "\x1B[5~":
		return TerminalKey{Type: TerminalKeyPageUp}
	case "\x1B[6~":
		return TerminalKey{Type: TerminalKeyPageDown}
	default:
		if value >= " " && value != "\x7F" {
			return TerminalKey{Type: TerminalKeyCharacter, Value: value}
		}
		return TerminalKey{Type: TerminalKeyIgnore}
	}
}

func readTerminalKey(ctx context.Context, input io.Reader, reader **bufio.Reader) (TerminalKey, error) {
	if keyInput, ok := input.(TerminalKeyReader); ok {
		return keyInput.ReadTerminalKey(ctx)
	}
	if *reader == nil {
		*reader = bufio.NewReader(input)
	}
	return readTerminalKeyBlocking(ctx, input, *reader)
}

func readTerminalKeyBlocking(ctx context.Context, input io.Reader, reader *bufio.Reader) (TerminalKey, error) {
	first, err := readByteWithContext(ctx, input, reader)
	if err != nil {
		return TerminalKey{}, err
	}
	if first != 0x1b {
		return ParseTerminalKey([]byte{first}), nil
	}

	sequence := []byte{first}
	for len(sequence) < 4 {
		next, err := readByteBriefly(ctx, input, reader)
		if err != nil {
			if errors.Is(err, errNoByteReady) || errors.Is(err, io.EOF) {
				break
			}
			return TerminalKey{}, err
		}
		sequence = append(sequence, next)
		if key := ParseTerminalKey(sequence); key.Type != TerminalKeyCharacter && key.Type != TerminalKeyIgnore {
			return key, nil
		}
	}
	return ParseTerminalKey(sequence), nil
}

var errNoByteReady = errors.New("no byte ready")

func readByteWithContext(ctx context.Context, input io.Reader, reader *bufio.Reader) (byte, error) {
	if err := waitForInputByte(ctx, input, -1); err != nil {
		return 0, err
	}
	return reader.ReadByte()
}

func readByteBriefly(ctx context.Context, input io.Reader, reader *bufio.Reader) (byte, error) {
	if reader.Buffered() > 0 {
		return reader.ReadByte()
	}
	if err := waitForInputByte(ctx, input, 15*time.Millisecond); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, errNoByteReady
		}
		return 0, err
	}
	return reader.ReadByte()
}

func waitForInputByte(ctx context.Context, input io.Reader, timeout time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return waitForTerminalInputByte(ctx, input, timeout)
}
