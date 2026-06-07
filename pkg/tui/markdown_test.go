package tui

import "testing"

func TestRenderMarkdownBlocksAndInlineStyles(t *testing.T) {
	got := RenderMarkdown("# Title\n## Section\n### Detail\n- item\n* other\n+ extra\n> quote")
	want := "█ Title\n■ Section\n▶ Detail\n• item\n• other\n• extra\n│ quote"
	if got != want {
		t.Fatalf("blocks = %q, want %q", got, want)
	}

	got = RenderMarkdown("Use **bold**, *italic*, and `code`.")
	want = "Use \x1b[1mbold\x1b[22m, \x1b[3mitalic\x1b[23m, and code."
	if got != want {
		t.Fatalf("inline = %q, want %q", got, want)
	}

	got = RenderMarkdown("```go\nfmt.Println(\"hi\")\n```")
	want = "```go\nfmt.Println(\"hi\")\n```"
	if got != want {
		t.Fatalf("code block = %q, want %q", got, want)
	}

	if got := RenderMarkdown("*"); got != "*" {
		t.Fatalf("partial list marker = %q, want *", got)
	}
	if got := RenderMarkdown("* "); got != "•" {
		t.Fatalf("complete list marker = %q, want bullet", got)
	}
}

func TestRenderMarkdownTables(t *testing.T) {
	got := RenderMarkdown("| Feature | Detail |\n| :--- | :--- |\n| Language | German (Swiss German dialect) |\n| Currency | Swiss Franc (CHF) |")
	want := "\x1b[1mFeature\x1b[22m   \x1b[1mDetail\x1b[22m                       \n" +
		"────────  ─────────────────────────────\n" +
		"Language  German (Swiss German dialect)\n" +
		"Currency  Swiss Franc (CHF)            "
	if got != want {
		t.Fatalf("table = %q, want %q", got, want)
	}
}
