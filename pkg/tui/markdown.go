package tui

import (
	"regexp"
	"strings"
)

const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiBoldOff   = "\x1b[22m"
	ansiItalic    = "\x1b[3m"
	ansiItalicOff = "\x1b[23m"
)

var (
	inlineCodePattern       = regexp.MustCompile("`([^`]+)`")
	boldAsteriskPattern     = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	boldUnderscorePattern   = regexp.MustCompile(`__([^_\n]+)__`)
	italicAsteriskPattern   = regexp.MustCompile(`\*([^*\n]+)\*`)
	italicUnderscorePattern = regexp.MustCompile(`_([^_\n]+)_`)
	unorderedListPattern    = regexp.MustCompile(`^(\s*)[-+*]\s+(.*)$`)
	numberedListPattern     = regexp.MustCompile(`^(\d+)\. (.*)$`)
)

// RenderMarkdown renders lightweight Markdown as ANSI terminal text.
func RenderMarkdown(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if table, ok := parseMarkdownTable(lines, index); ok {
			out = append(out, renderMarkdownTable(table)...)
			index = table.endIndex - 1
			continue
		}
		out = append(out, renderMarkdownLine(line))
	}
	return strings.Join(out, "\n")
}

func renderMarkdownLine(line string) string {
	switch {
	case strings.HasPrefix(line, "### "):
		return renderInlineMarkdown("▶ " + line[4:])
	case strings.HasPrefix(line, "## "):
		return renderInlineMarkdown("■ " + line[3:])
	case strings.HasPrefix(line, "# "):
		return renderInlineMarkdown("█ " + line[2:])
	case strings.HasPrefix(line, "> "):
		return renderInlineMarkdown("│ " + line[2:])
	}
	if m := unorderedListPattern.FindStringSubmatch(line); m != nil {
		text := m[2]
		if text == "" {
			return renderInlineMarkdown(m[1] + "•")
		}
		return renderInlineMarkdown(m[1] + "• " + text)
	}
	if m := numberedListPattern.FindStringSubmatch(line); m != nil {
		return renderInlineMarkdown(m[1] + ". " + m[2])
	}
	return renderInlineMarkdown(line)
}

func renderInlineMarkdown(input string) string {
	input = inlineCodePattern.ReplaceAllString(input, "$1")
	input = boldAsteriskPattern.ReplaceAllString(input, ansiBold+"$1"+ansiBoldOff)
	input = boldUnderscorePattern.ReplaceAllString(input, ansiBold+"$1"+ansiBoldOff)
	input = italicAsteriskPattern.ReplaceAllString(input, ansiItalic+"$1"+ansiItalicOff)
	input = italicUnderscorePattern.ReplaceAllString(input, ansiItalic+"$1"+ansiItalicOff)
	return input
}

type tableAlignment string

const (
	tableAlignLeft   tableAlignment = "left"
	tableAlignCenter tableAlignment = "center"
	tableAlignRight  tableAlignment = "right"
)

type parsedMarkdownTable struct {
	alignments []tableAlignment
	endIndex   int
	header     []string
	rows       [][]string
}

func parseMarkdownTable(lines []string, start int) (parsedMarkdownTable, bool) {
	header, ok := parseMarkdownTableCells(lines[start])
	if !ok || len(header) < 2 || start+1 >= len(lines) {
		return parsedMarkdownTable{}, false
	}
	separator, ok := parseMarkdownTableCells(lines[start+1])
	if !ok || len(separator) != len(header) {
		return parsedMarkdownTable{}, false
	}
	alignments, ok := parseMarkdownTableAlignments(separator)
	if !ok {
		return parsedMarkdownTable{}, false
	}
	table := parsedMarkdownTable{alignments: alignments, header: header, endIndex: start + 2}
	for table.endIndex < len(lines) {
		row, ok := parseMarkdownTableCells(lines[table.endIndex])
		if !ok {
			break
		}
		table.rows = append(table.rows, normalizeMarkdownTableRow(row, len(header)))
		table.endIndex++
	}
	return table, true
}

func parseMarkdownTableCells(line string) ([]string, bool) {
	if !strings.Contains(line, "|") {
		return nil, false
	}
	tableLine := strings.TrimSpace(line)
	if strings.HasPrefix(tableLine, "|") {
		tableLine = tableLine[1:]
	}
	if strings.HasSuffix(tableLine, "|") && !strings.HasSuffix(tableLine, `\|`) {
		tableLine = tableLine[:len(tableLine)-1]
	}
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(tableLine); i++ {
		ch := tableLine[i]
		if ch == '\\' && i+1 < len(tableLine) && tableLine[i+1] == '|' {
			cell.WriteByte('|')
			i++
			continue
		}
		if ch == '|' {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteByte(ch)
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	return cells, true
}

func parseMarkdownTableAlignments(cells []string) ([]tableAlignment, bool) {
	var alignments []tableAlignment
	for _, cell := range cells {
		if !regexp.MustCompile(`^:?-{3,}:?$`).MatchString(cell) {
			return nil, false
		}
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			alignments = append(alignments, tableAlignCenter)
		case right:
			alignments = append(alignments, tableAlignRight)
		default:
			alignments = append(alignments, tableAlignLeft)
		}
	}
	return alignments, true
}

func normalizeMarkdownTableRow(row []string, length int) []string {
	out := make([]string, length)
	copy(out, row)
	return out
}

func renderMarkdownTable(table parsedMarkdownTable) []string {
	header := make([]string, len(table.header))
	for i, cell := range table.header {
		header[i] = ansiBold + renderInlineMarkdown(cell) + ansiBoldOff
	}
	rows := make([][]string, len(table.rows))
	for i, row := range table.rows {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			rows[i][j] = renderInlineMarkdown(cell)
		}
	}
	widths := make([]int, len(table.alignments))
	for i := range widths {
		widths[i] = 3
		if i < len(header) && visibleLength(header[i]) > widths[i] {
			widths[i] = visibleLength(header[i])
		}
		for _, row := range rows {
			if i < len(row) && visibleLength(row[i]) > widths[i] {
				widths[i] = visibleLength(row[i])
			}
		}
	}
	out := []string{
		formatMarkdownTableRow(header, widths, table.alignments),
		renderMarkdownTableSeparator(widths),
	}
	for _, row := range rows {
		out = append(out, formatMarkdownTableRow(row, widths, table.alignments))
	}
	return out
}

func renderMarkdownTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("─", width)
	}
	return strings.Join(parts, "  ")
}

func formatMarkdownTableRow(row []string, widths []int, alignments []tableAlignment) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		parts[i] = alignMarkdownTableCell(cell, width, alignments[i])
	}
	return strings.Join(parts, "  ")
}

func alignMarkdownTableCell(cell string, width int, alignment tableAlignment) string {
	padding := maxInt(0, width-visibleLength(cell))
	switch alignment {
	case tableAlignRight:
		return strings.Repeat(" ", padding) + cell
	case tableAlignCenter:
		left := padding / 2
		right := padding - left
		return strings.Repeat(" ", left) + cell + strings.Repeat(" ", right)
	default:
		return cell + strings.Repeat(" ", padding)
	}
}
