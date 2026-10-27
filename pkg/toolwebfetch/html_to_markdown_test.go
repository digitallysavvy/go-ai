package toolwebfetch

import (
	"net/url"
	"strings"
	"testing"
)

func TestHTMLToMarkdownElements(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/page")
	src := page(`<main>
	  <h2>Section</h2>
	  <p>A <strong>bold</strong> and <em>soft</em> word with <code>code</code> and a <a href="../up">link</a>,
	  an <a href="javascript:alert(1)">unsafe link</a> and <img src="/i.png" alt="pic">.</p>
	  <ul><li>One</li><li>Two<ul><li>Nested</li></ul></li></ul>
	  <ol start="3"><li>Third</li></ol>
	  <pre><code class="language-go">fmt.Println("hi")</code></pre>
	  <blockquote><p>Quoted</p></blockquote>
	  <table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
	  <script>alert(1)</script><style>p{}</style><!-- comment -->
	</main>`, "<title>T</title>")
	_, content, err := htmlToMarkdown(src, base, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Section",
		"**bold**",
		"*soft*",
		"`code`",
		"[link](https://example.com/up)",
		"unsafe link", // text kept, URL dropped
		"![pic](https://example.com/i.png)",
		"- One",
		"- Two\n  - Nested",
		"3. Third",
		"```go\nfmt.Println(\"hi\")\n```",
		"> Quoted",
		"| A | B |\n| --- | --- |\n| 1 | 2 |",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content lacks %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"alert", "p{}", "comment", "javascript:"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("content contains %q:\n%s", unwanted, content)
		}
	}
}

func TestHTMLToMarkdownTruncatesHugePages(t *testing.T) {
	src := "<html><body><p>" + strings.Repeat("x", maxHTMLLength+10) + "</p></body></html>"
	_, content, err := htmlToMarkdown(src, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(content, "was truncated before conversion.]") {
		t.Fatalf("tail = %q", content[len(content)-80:])
	}
}

func TestContentSelectorSyntax(t *testing.T) {
	src := page(`<div class="a b" id="x"><p>inner</p></div><section><p>other</p></section>`, "")
	for selector, want := range map[string]string{
		"#x":           "inner",
		".b":           "inner",
		"div.a":        "inner",
		"section":      "other",
		"[id=x]":       "inner",
		".missing, #x": "inner",
		"div > p":      "inner other", // unsupported syntax never matches: whole body
	} {
		_, content, _ := htmlToMarkdown(src, nil, selector)
		if !strings.Contains(content, strings.Fields(want)[0]) {
			t.Errorf("selector %q: content %q", selector, content)
		}
	}
	_, content, _ := htmlToMarkdown(src, nil, "div > p")
	if !strings.Contains(content, "other") {
		t.Errorf("unsupported selector should fall back to the page, got %q", content)
	}
}
