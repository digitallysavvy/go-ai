package tui

import "testing"

// TestSanitizeTerminalTextPreservesUnicodeAndExpandsTabs mirrors TS
// sanitize-terminal-text.test.ts's "preserves printable Unicode and
// expands tabs to spaces".
func TestSanitizeTerminalTextPreservesUnicodeAndExpandsTabs(t *testing.T) {
	got := sanitizeTerminalText("Hello\t日本語 😀 é", false)
	want := "Hello    日本語 😀 é"
	if got != want {
		t.Fatalf("sanitizeTerminalText() = %q, want %q", got, want)
	}
}

// TestSanitizeTerminalTextNewlineHandling mirrors TS's "preserves newlines
// only in multiline content".
func TestSanitizeTerminalTextNewlineHandling(t *testing.T) {
	if got, want := sanitizeTerminalText("one\ntwo", false), "one\\u000atwo"; got != want {
		t.Fatalf("sanitizeTerminalText(multiline=false) = %q, want %q", got, want)
	}
	if got, want := sanitizeTerminalText("one\ntwo", true), "one\ntwo"; got != want {
		t.Fatalf("sanitizeTerminalText(multiline=true) = %q, want %q", got, want)
	}
	if got, want := sanitizeTerminalText("one\rtwo", true), "one\\u000dtwo"; got != want {
		t.Fatalf("sanitizeTerminalText(\\r, multiline=true) = %q, want %q", got, want)
	}
}

// TestSanitizeTerminalTextNeutralizesControlSequencesAcrossChunkBoundaries
// mirrors TS's "neutralizes %s regardless of chunk boundaries" table:
// every escape-sequence family the TUI must never forward raw, verified to
// (1) contain no raw control bytes after sanitizing, and (2) produce the
// same result whether sanitized whole or split at any point and
// concatenated — proving a sequence split across stream chunks is still
// neutralized without needing terminal parser state.
func TestSanitizeTerminalTextNeutralizesControlSequencesAcrossChunkBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"OSC with BEL", "\x1b]52;c;dGVzdA==\x07"},
		{"OSC with ST", "\x1b]0;title\x1b\\"},
		{"DCS", "\x1bPtest\x1b\\"},
		{"APC", "\x1b_test\x1b\\"},
		{"PM", "\x1b^test\x1b\\"},
		{"SOS", "\x1bXtest\x1b\\"},
		{"C1 controls", "\x90test\x9c\x9dtest\x9c\x9f test\x9c"},
		{"CSI cursor movement", "\x1b[1;1H\x9b2J"},
		{"SGR conceal", "\x1b[8mhidden\x1b[0m"},
		{"terminal reset", "\x1bc"},
		{"unterminated OSC", "\x1b]52;c;"},
		{"trailing escape", "text\x1b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := sanitizeTerminalText(tc.input, false)
			for _, r := range sanitized {
				if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
					t.Fatalf("sanitized output still contains raw control code point 0x%02x: %q", r, sanitized)
				}
			}
			for split := 0; split <= len(tc.input); split++ {
				got := sanitizeTerminalText(tc.input[:split], false) + sanitizeTerminalText(tc.input[split:], false)
				if got != sanitized {
					t.Fatalf("split at %d: sanitizing halves separately = %q, want %q (sanitized whole)", split, got, sanitized)
				}
			}
		})
	}
}

// TestSanitizeTerminalTextEscapesAllC0DelAndC1ControlsExceptTabs mirrors
// TS's "escapes all C0, DEL, and C1 controls except tabs".
func TestSanitizeTerminalTextEscapesAllC0DelAndC1ControlsExceptTabs(t *testing.T) {
	for code := 0; code <= 0x9f; code++ {
		if code >= 0x20 && code < 0x7f {
			continue
		}
		got := sanitizeTerminalText(string(rune(code)), false)
		want := "    "
		if code != 0x09 {
			want = escapedControl(rune(code))
		}
		if got != want {
			t.Fatalf("sanitizeTerminalText(0x%02x) = %q, want %q", code, got, want)
		}
	}
}
