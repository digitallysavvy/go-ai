package tui

import "strings"

// sanitizeTerminalText escapes untrusted terminal control characters before
// adding any TUI-owned ANSI styles or layout (hash 8e0fbcc10a). Agent text,
// tool content, errors, and titles can contain C0, DEL, or C1 control
// characters; passed through unescaped, a control sequence (e.g. an OSC
// clipboard write, a DCS/APC/PM/SOS sequence, or an SGR conceal) could
// change terminal state instead of being displayed as text.
//
// Escaping every individual control character, rather than parsing control
// sequences, also makes incomplete sequences and sequences split across
// stream chunks safe without maintaining terminal parser state: splitting
// the input at any point and sanitizing each half separately yields the
// same result as sanitizing it whole.
//
// Sanitization affects display only; callers must not persist or return the
// sanitized string in place of the original (e.g. a submitted prompt or a
// tool's stored input/output).
//
// When multiline is true, '\n' is preserved so multi-line content (e.g.
// Markdown) keeps its line structure; '\r' and every other control
// character are still escaped. When multiline is false, '\n' is escaped
// like any other control character, since the caller is rendering a single
// display line (a title, a status/input line, or a border label).
func sanitizeTerminalText(input string, multiline bool) string {
	var out strings.Builder
	out.Grow(len(input))
	for _, r := range input {
		switch {
		case r == '\n' && multiline:
			out.WriteByte('\n')
		case r == '\t':
			out.WriteString("    ")
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			out.WriteString(escapedControl(r))
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

const hexDigits = "0123456789abcdef"

// escapedControl renders a control code point as \\uXXXX (4 lowercase hex
// digits), matching TS sanitizeTerminalText's
// `\\u${code.toString(16).padStart(4, '0')}`. Every code point this is
// called with (C0, DEL, C1) fits in one UTF-16 code unit, so a single
// \uXXXX escape always suffices.
func escapedControl(r rune) string {
	b := [6]byte{'\\', 'u', '0', '0', '0', '0'}
	code := uint32(r)
	b[5] = hexDigits[code&0xf]
	b[4] = hexDigits[(code>>4)&0xf]
	return string(b[:])
}
