package tui

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(input string) string {
	return ansiPattern.ReplaceAllString(input, "")
}

func visibleLength(input string) int {
	width := 0
	for i := 0; i < len(input); {
		if input[i] == 0x1b {
			loc := ansiPattern.FindStringIndex(input[i:])
			if loc != nil && loc[0] == 0 {
				i += loc[1]
				continue
			}
		}
		r, size := nextRune(input[i:])
		width += codePointWidth(r)
		i += size
	}
	return width
}

func sliceVisible(input string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	visible := 0
	i := 0
	for i < len(input) {
		if input[i] == 0x1b {
			loc := ansiPattern.FindStringIndex(input[i:])
			if loc != nil && loc[0] == 0 {
				out.WriteString(input[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, size := nextRune(input[i:])
		characterWidth := codePointWidth(r)
		if characterWidth > 0 && visible+characterWidth > width {
			break
		}
		out.WriteRune(r)
		visible += characterWidth
		i += size
	}
	for i < len(input) {
		loc := ansiPattern.FindStringIndex(input[i:])
		if loc == nil || loc[0] != 0 {
			break
		}
		out.WriteString(input[i : i+loc[1]])
		i += loc[1]
	}
	return out.String()
}

func wrapVisibleLine(line string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if line == "" {
		return []string{""}
	}
	var lines []string
	remaining := line
	for visibleLength(remaining) > width {
		breakAt := findVisibleBreakPoint(remaining, width)
		lines = append(lines, strings.TrimRight(remaining[:breakAt], " "))
		remaining = strings.TrimLeft(remaining[breakAt:], " ")
	}
	lines = append(lines, remaining)
	return lines
}

func nextRune(input string) (rune, int) {
	for _, r := range input {
		return r, len(string(r))
	}
	return 0, 0
}

func codePointWidth(r rune) int {
	if r == '\t' {
		return 4
	}
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if isZeroWidthCodePoint(r) {
		return 0
	}
	if isWideCodePoint(r) {
		return 2
	}
	return 1
}

func isZeroWidthCodePoint(r rune) bool {
	return (r >= 0x0300 && r <= 0x036f) ||
		(r >= 0x0483 && r <= 0x0489) ||
		(r >= 0x0591 && r <= 0x05bd) ||
		r == 0x05bf ||
		(r >= 0x05c1 && r <= 0x05c2) ||
		(r >= 0x05c4 && r <= 0x05c5) ||
		r == 0x05c7 ||
		(r >= 0x0610 && r <= 0x061a) ||
		(r >= 0x064b && r <= 0x065f) ||
		r == 0x0670 ||
		(r >= 0x06d6 && r <= 0x06dc) ||
		(r >= 0x06df && r <= 0x06e4) ||
		(r >= 0x06e7 && r <= 0x06e8) ||
		(r >= 0x06ea && r <= 0x06ed) ||
		r == 0x0711 ||
		(r >= 0x0730 && r <= 0x074a) ||
		(r >= 0x07a6 && r <= 0x07b0) ||
		(r >= 0x07eb && r <= 0x07f3) ||
		(r >= 0x0816 && r <= 0x0819) ||
		(r >= 0x081b && r <= 0x0823) ||
		(r >= 0x0825 && r <= 0x0827) ||
		(r >= 0x0829 && r <= 0x082d) ||
		(r >= 0x0859 && r <= 0x085b) ||
		(r >= 0x08d3 && r <= 0x0902) ||
		r == 0x093a ||
		r == 0x093c ||
		(r >= 0x0941 && r <= 0x0948) ||
		r == 0x094d ||
		(r >= 0x0951 && r <= 0x0957) ||
		r == 0x200d ||
		(r >= 0xfe00 && r <= 0xfe0f) ||
		(r >= 0xe0100 && r <= 0xe01ef)
}

func isWideCodePoint(r rune) bool {
	return r >= 0x1100 &&
		(r <= 0x115f ||
			r == 0x2329 ||
			r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe19) ||
			(r >= 0xfe30 && r <= 0xfe6f) ||
			(r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x1f300 && r <= 0x1f64f) ||
			(r >= 0x1f900 && r <= 0x1f9ff) ||
			(r >= 0x20000 && r <= 0x3fffd))
}

func findVisibleBreakPoint(input string, width int) int {
	slice := sliceVisible(input, width+1)
	if lastSpace := strings.LastIndex(slice, " "); lastSpace > 0 {
		return lastSpace
	}
	return len(sliceVisible(input, width))
}
