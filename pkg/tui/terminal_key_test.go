package tui

import "testing"

func TestParseTerminalKeyDecodesControlKeys(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want TerminalKey
	}{
		{name: "up", raw: "\x1B[A", want: TerminalKey{Type: TerminalKeyUp}},
		{name: "down", raw: "\x1B[B", want: TerminalKey{Type: TerminalKeyDown}},
		{name: "page up", raw: "\x1B[5~", want: TerminalKey{Type: TerminalKeyPageUp}},
		{name: "page down", raw: "\x1B[6~", want: TerminalKey{Type: TerminalKeyPageDown}},
		{name: "backspace", raw: "\u007f", want: TerminalKey{Type: TerminalKeyBackspace}},
		{name: "enter cr", raw: "\r", want: TerminalKey{Type: TerminalKeyEnter}},
		{name: "enter lf", raw: "\n", want: TerminalKey{Type: TerminalKeyEnter}},
		{name: "ctrl l", raw: "\u000c", want: TerminalKey{Type: TerminalKeyCtrlL}},
		{name: "ctrl c", raw: "\u0003", want: TerminalKey{Type: TerminalKeyCtrlC}},
		{name: "escape", raw: "\x1B", want: TerminalKey{Type: TerminalKeyEscape}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTerminalKey([]byte(tt.raw))
			if got != tt.want {
				t.Fatalf("ParseTerminalKey(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseTerminalKeyKeepsPrintableInput(t *testing.T) {
	got := ParseTerminalKey([]byte("hello"))
	want := TerminalKey{Type: TerminalKeyCharacter, Value: "hello"}
	if got != want {
		t.Fatalf("ParseTerminalKey printable = %#v, want %#v", got, want)
	}
}
