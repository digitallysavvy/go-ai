package toolwebfetch

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Larger HTML is truncated before conversion. Extraction time and memory grow
// quickly with document size.
const maxHTMLLength = 2_000_000

// Documents nested deeper than this fall back to plain text.
const maxDOMDepth = 512

// htmlToMarkdown extracts the main content of an HTML page and converts it to
// markdown. It stands in for the TypeScript tool's defuddle: it removes
// navigation, footers and other page chrome, prefers <main>, <article> or an
// explicit contentSelector, and resolves relative links against pageURL.
func htmlToMarkdown(source string, pageURL *url.URL, contentSelector string) (title string, content string, err error) {
	truncated := false
	if len(source) > maxHTMLLength {
		if runes := []rune(source); len(runes) > maxHTMLLength {
			source = string(runes[:maxHTMLLength])
			truncated = true
		}
	}

	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		// The HTML parser refuses documents that nest elements too deeply.
		// Such documents fall back to plain text, like the depth check below.
		title, content = tokenText(source)
		return title, finishContent(content, truncated), nil
	}

	if documentDepth(doc) > maxDOMDepth {
		title = documentTitle(doc, nil)
		content = plainText(doc)
	} else {
		root := selectMainContent(doc, contentSelector)
		title = documentTitle(doc, root)
		pruneNoise(root, contentSelector != "" && root != nil)
		content = (&markdownWriter{base: pageURL}).render(root)
	}

	return title, finishContent(content, truncated), nil
}

func finishContent(content string, truncated bool) string {
	if truncated {
		content += fmt.Sprintf("\n\n[The page exceeded %d characters of HTML and was truncated before conversion.]", maxHTMLLength)
	}
	return strings.TrimSpace(content)
}

// tokenText extracts the page title and text with the tokenizer, which has no
// nesting limit and runs in linear time.
func tokenText(source string) (title, content string) {
	z := html.NewTokenizer(strings.NewReader(source))
	var text, titleText strings.Builder
	skipDepth := 0 // inside script/style/noscript/template
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return collapseSpace(titleText.String()), normalizeWhitespace(text.String())
		case html.StartTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "template":
				skipDepth++
			case "title":
				inTitle = true
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "template":
				if skipDepth > 0 {
					skipDepth--
				}
			case "title":
				inTitle = false
			}
		case html.TextToken:
			switch {
			case inTitle:
				titleText.Write(z.Text())
			case skipDepth == 0:
				text.Write(z.Text())
			}
		}
	}
}

// documentDepth returns the maximum element nesting depth, stopping early
// once it exceeds maxDOMDepth. It is iterative: untrusted input can nest
// deeply.
func documentDepth(root *html.Node) int {
	type frame struct {
		node  *html.Node
		depth int
	}
	maxDepth := 0
	stack := []frame{{root, 0}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		depth := f.depth
		if f.node.Type == html.ElementNode {
			depth++
			if depth > maxDepth {
				maxDepth = depth
				if maxDepth > maxDOMDepth {
					return maxDepth
				}
			}
		}
		for c := f.node.FirstChild; c != nil; c = c.NextSibling {
			stack = append(stack, frame{c, depth})
		}
	}
	return maxDepth
}

func findFirst(root *html.Node, match func(*html.Node) bool) *html.Node {
	stack := []*html.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == html.ElementNode && match(n) {
			return n
		}
		// push children in reverse so the first child is visited first
		var kids []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			kids = append(kids, c)
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, kids[i])
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// matchesSimpleSelector supports compound selectors made of a tag name, #id,
// .class and [attr] / [attr=value] parts, and comma-separated lists of them.
func matchesSelector(n *html.Node, selector string) bool {
	for _, alt := range strings.Split(selector, ",") {
		if matchesCompound(n, strings.TrimSpace(alt)) {
			return true
		}
	}
	return false
}

var compoundPart = regexp.MustCompile(`^(?:([a-zA-Z][a-zA-Z0-9-]*)|#([\w-]+)|\.([\w-]+)|\[([\w-]+)(?:=["']?([^"'\]]*)["']?)?\])`)

func matchesCompound(n *html.Node, compound string) bool {
	if compound == "" {
		return false
	}
	rest := compound
	first := true
	for rest != "" {
		m := compoundPart.FindStringSubmatch(rest)
		if m == nil {
			return false // unsupported syntax (combinators, pseudo-classes)
		}
		switch {
		case m[1] != "":
			if !first || !strings.EqualFold(n.Data, m[1]) {
				return false
			}
		case m[2] != "":
			if attr(n, "id") != m[2] {
				return false
			}
		case m[3] != "":
			found := false
			for _, c := range strings.Fields(attr(n, "class")) {
				if c == m[3] {
					found = true
				}
			}
			if !found {
				return false
			}
		case m[4] != "":
			if !hasAttr(n, m[4]) {
				return false
			}
			if strings.Contains(m[0], "=") && attr(n, m[4]) != m[5] {
				return false
			}
		}
		first = false
		rest = rest[len(m[0]):]
	}
	return true
}

var mainContentSelectors = []string{
	"main", "article", "[role=main]", "#content", "#main-content", ".post-content",
	".entry-content", ".article-body", ".article-content", ".markdown-body",
}

func findBody(doc *html.Node) *html.Node {
	return findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body })
}

// selectMainContent picks the element to convert: the contentSelector match,
// else the first common main-content container, else the body.
func selectMainContent(doc *html.Node, contentSelector string) *html.Node {
	if contentSelector != "" {
		if n := findFirst(doc, func(n *html.Node) bool { return matchesSelector(n, contentSelector) }); n != nil {
			return n
		}
	}
	for _, sel := range mainContentSelectors {
		if n := findFirst(doc, func(n *html.Node) bool { return matchesCompound(n, sel) }); n != nil {
			return n
		}
	}
	if body := findBody(doc); body != nil {
		return body
	}
	return doc
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapseSpace(s string) string {
	return strings.Join(strings.FieldsFunc(s, isSpace), " ")
}

// isSpace is JavaScript \s: Unicode white space plus U+FEFF, without U+0085.
func isSpace(r rune) bool { return r != 0x85 && (unicode.IsSpace(r) || r == 0xFEFF) }

// documentTitle returns the page title. A "Page | Site" title is shortened to
// the page heading when the heading is one of its parts.
func documentTitle(doc *html.Node, root *html.Node) string {
	titleNode := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Title })
	title := ""
	if titleNode != nil {
		title = collapseSpace(textOf(titleNode))
	}
	scope := root
	if scope == nil {
		scope = doc
	}
	if h1 := findFirst(scope, func(n *html.Node) bool { return n.DataAtom == atom.H1 }); h1 != nil {
		heading := collapseSpace(textOf(h1))
		if heading != "" && title != "" && title != heading && strings.Contains(title, heading) {
			return heading
		}
		if title == "" {
			return heading
		}
	}
	return title
}

var noiseTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Nav: true, atom.Footer: true, atom.Aside: true, atom.Iframe: true,
	atom.Svg: true, atom.Canvas: true, atom.Form: true, atom.Button: true,
	atom.Select: true, atom.Textarea: true, atom.Dialog: true, atom.Input: true,
	atom.Head: true, atom.Link: true, atom.Meta: true, atom.Object: true, atom.Embed: true,
}

// pruneNoise removes page chrome below root. The root itself is kept, so an
// explicit selection such as "#sidebar" (an <aside>) still converts.
func pruneNoise(root *html.Node, explicit bool) {
	if root == nil {
		return
	}
	var remove []*html.Node
	stack := []*html.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.CommentNode || c.Type == html.DoctypeNode {
				remove = append(remove, c)
				continue
			}
			if c.Type == html.ElementNode {
				if noiseTags[c.DataAtom] || hasAttr(c, "hidden") || attr(c, "aria-hidden") == "true" ||
					(c.DataAtom == atom.Header && root.DataAtom == atom.Body && !explicit) {
					remove = append(remove, c)
					continue
				}
			}
			stack = append(stack, c)
		}
	}
	for _, n := range remove {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
}

// plainText returns the page text for documents too deeply nested to convert.
// Whitespace is normalized in linear time (fetched content is untrusted).
func plainText(doc *html.Node) string {
	root := findBody(doc)
	if root == nil {
		root = doc
	}
	var b strings.Builder
	stack := []*html.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			continue
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Template:
				continue
			}
		}
		var kids []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			kids = append(kids, c)
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, kids[i])
		}
	}
	return normalizeWhitespace(b.String())
}

// normalizeWhitespace collapses runs of non-newline whitespace to one space
// and any newline-containing run to a single newline, then trims.
func normalizeWhitespace(s string) string {
	var out strings.Builder
	pendingSpace, pendingNewline := false, false
	for _, r := range s {
		switch {
		case r == '\n':
			pendingNewline = true
		case isSpace(r):
			pendingSpace = true
		default:
			if out.Len() > 0 {
				if pendingNewline {
					out.WriteByte('\n')
				} else if pendingSpace {
					out.WriteByte(' ')
				}
			}
			pendingSpace, pendingNewline = false, false
			out.WriteRune(r)
		}
	}
	return out.String()
}

// markdownWriter renders an element tree as markdown.
type markdownWriter struct {
	base *url.URL
	out  strings.Builder
}

func (w *markdownWriter) render(root *html.Node) string {
	if root == nil {
		return ""
	}
	w.block(root, 0, "")
	text := w.out.String()
	text = blankLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

var blankLines = regexp.MustCompile(`\n{3,}`)

func (w *markdownWriter) resolve(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || w.base == nil {
		return ref
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "vbscript:") {
		return ""
	}
	if u, err := w.base.Parse(ref); err == nil {
		return u.String()
	}
	return ref
}

func (w *markdownWriter) newParagraph() {
	s := w.out.String()
	switch {
	case s == "":
	case strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		w.out.WriteString("\n")
	default:
		w.out.WriteString("\n\n")
	}
}

// block converts n and its children. prefix is prepended to every line
// (list indentation, blockquote markers).
func (w *markdownWriter) block(n *html.Node, depth int, prefix string) {
	if depth > maxDOMDepth {
		return
	}
	switch n.Type {
	case html.TextNode:
		if text := collapseSpace(n.Data); text != "" {
			w.out.WriteString(prefixLines(text, prefix))
		}
		return
	case html.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.block(c, depth+1, prefix)
		}
		return
	}

	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level, _ := strconv.Atoi(n.Data[1:])
		if text := w.inline(n); text != "" {
			w.newParagraph()
			w.out.WriteString(prefixLines(strings.Repeat("#", level)+" "+text, prefix))
			w.out.WriteString("\n\n")
		}
	case atom.P:
		if text := w.inline(n); text != "" {
			w.newParagraph()
			w.out.WriteString(prefixLines(text, prefix))
			w.out.WriteString("\n\n")
		}
	case atom.Pre:
		code := strings.Trim(textOf(n), "\n")
		lang := ""
		if codeNode := findFirst(n, func(c *html.Node) bool { return c.DataAtom == atom.Code }); codeNode != nil {
			for _, class := range strings.Fields(attr(codeNode, "class")) {
				if strings.HasPrefix(class, "language-") {
					lang = strings.TrimPrefix(class, "language-")
				}
			}
		}
		w.newParagraph()
		fence := "```"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		w.out.WriteString(prefixLines(fence+lang+"\n"+code+"\n"+fence, prefix))
		w.out.WriteString("\n\n")
	case atom.Blockquote:
		inner := &markdownWriter{base: w.base}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inner.block(c, depth+1, "")
		}
		if text := strings.TrimSpace(inner.out.String()); text != "" {
			w.newParagraph()
			w.out.WriteString(prefixLines(prefixLines(text, "> "), prefix))
			w.out.WriteString("\n\n")
		}
	case atom.Ul, atom.Ol:
		w.list(n, depth, prefix, false)
	case atom.Hr:
		w.newParagraph()
		w.out.WriteString(prefixLines("---", prefix) + "\n\n")
	case atom.Br:
		w.out.WriteString("\n")
	case atom.Table:
		w.table(n, prefix)
	case atom.Img:
		if text := w.inline(n); text != "" {
			w.out.WriteString(prefixLines(text, prefix))
		}
	case atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Body, atom.Html,
		atom.Figure, atom.Figcaption, atom.Details, atom.Summary, atom.Dl, atom.Dd, atom.Dt, atom.Address, atom.Li:
		w.container(n, depth, prefix)
	default:
		w.container(n, depth, prefix)
	}
}

var blockAtoms = map[atom.Atom]bool{
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.P: true, atom.Pre: true, atom.Blockquote: true, atom.Ul: true, atom.Ol: true, atom.Hr: true,
	atom.Table: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Header: true, atom.Body: true, atom.Html: true, atom.Figure: true, atom.Figcaption: true,
	atom.Details: true, atom.Summary: true, atom.Dl: true, atom.Dd: true, atom.Dt: true,
	atom.Address: true, atom.Li: true, atom.Footer: true, atom.Nav: true, atom.Aside: true,
}

// container converts a generic block: block-level children become blocks and
// runs of inline children become one paragraph.
func (w *markdownWriter) container(n *html.Node, depth int, prefix string) {
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		text := strings.TrimSpace(w.inlineNodes(run))
		run = nil
		if text != "" {
			w.newParagraph()
			w.out.WriteString(prefixLines(text, prefix))
			w.out.WriteString("\n\n")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && blockAtoms[c.DataAtom] {
			flush()
			w.block(c, depth+1, prefix)
		} else {
			run = append(run, c)
		}
	}
	flush()
}

func (w *markdownWriter) list(n *html.Node, depth int, prefix string, nested bool) {
	ordered := n.DataAtom == atom.Ol
	index := 1
	if start, err := strconv.Atoi(attr(n, "start")); ordered && err == nil {
		index = start
	}
	if !nested {
		w.newParagraph()
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Li {
			continue
		}
		marker := "- "
		if ordered {
			marker = strconv.Itoa(index) + ". "
			index++
		}
		inner := &markdownWriter{base: w.base}
		var run []*html.Node
		flush := func() {
			if len(run) == 0 {
				return
			}
			if text := strings.TrimSpace(inner.inlineNodes(run)); text != "" {
				inner.out.WriteString(text + "\n")
			}
			run = nil
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			if k.Type == html.ElementNode && (k.DataAtom == atom.Ul || k.DataAtom == atom.Ol) {
				flush()
				inner.list(k, depth+1, "", true)
			} else if k.Type == html.ElementNode && blockAtoms[k.DataAtom] {
				flush()
				inner.block(k, depth+1, "")
			} else {
				run = append(run, k)
			}
		}
		flush()
		text := strings.TrimSpace(inner.out.String())
		if text == "" {
			continue
		}
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			switch {
			case i == 0:
				w.out.WriteString(prefix + marker + line + "\n")
			case line == "":
				w.out.WriteString("\n")
			default:
				w.out.WriteString(prefix + strings.Repeat(" ", len(marker)) + line + "\n")
			}
		}
	}
	if !nested {
		w.out.WriteString("\n")
	}
}

func (w *markdownWriter) table(n *html.Node, prefix string) {
	var rows [][]string
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Tr:
				var cells []string
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
						text := strings.ReplaceAll(w.inline(cell), "\n", " ")
						cells = append(cells, strings.ReplaceAll(text, "|", `\|`))
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			case atom.Thead, atom.Tbody, atom.Tfoot:
				collect(c)
			}
		}
	}
	collect(n)
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	line := func(r []string) string {
		for len(r) < width {
			r = append(r, "")
		}
		return "| " + strings.Join(r, " | ") + " |"
	}
	w.newParagraph()
	var b strings.Builder
	b.WriteString(line(rows[0]) + "\n")
	sep := make([]string, width)
	for i := range sep {
		sep[i] = "---"
	}
	b.WriteString(line(sep) + "\n")
	for _, r := range rows[1:] {
		b.WriteString(line(r) + "\n")
	}
	w.out.WriteString(prefixLines(strings.TrimRight(b.String(), "\n"), prefix))
	w.out.WriteString("\n\n")
}

func prefixLines(text, prefix string) string {
	if prefix == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func (w *markdownWriter) inline(n *html.Node) string {
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	if n.DataAtom == atom.Img {
		children = []*html.Node{n}
	}
	return strings.TrimSpace(w.inlineNodes(children))
}

// inlineNodes renders inline content with whitespace collapsed.
func (w *markdownWriter) inlineNodes(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		w.inlineNode(&b, n, 0)
	}
	return collapseKeepNewlines(b.String())
}

// collapseKeepNewlines collapses spaces but keeps the explicit line breaks
// produced by <br>.
func collapseKeepNewlines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(collapseSpace(l))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (w *markdownWriter) inlineNode(b *strings.Builder, n *html.Node, depth int) {
	if depth > maxDOMDepth {
		return
	}
	switch n.Type {
	case html.TextNode:
		b.WriteString(escapeMarkdown(n.Data))
		return
	case html.ElementNode:
	default:
		return
	}
	children := func() string {
		var cb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.inlineNode(&cb, c, depth+1)
		}
		return cb.String()
	}
	switch n.DataAtom {
	case atom.Br:
		b.WriteString("\n")
	case atom.A:
		text := strings.TrimSpace(collapseSpace(children()))
		href := w.resolve(attr(n, "href"))
		switch {
		case text == "":
		case href == "" || strings.HasPrefix(attr(n, "href"), "#"):
			b.WriteString(text)
		default:
			b.WriteString("[" + text + "](" + href + ")")
		}
	case atom.Img:
		src := w.resolve(attr(n, "src"))
		if src != "" {
			b.WriteString("![" + collapseSpace(attr(n, "alt")) + "](" + src + ")")
		}
	case atom.Strong, atom.B:
		if text := strings.TrimSpace(children()); text != "" {
			b.WriteString("**" + text + "**")
		}
	case atom.Em, atom.I:
		if text := strings.TrimSpace(children()); text != "" {
			b.WriteString("*" + text + "*")
		}
	case atom.Del, atom.S:
		if text := strings.TrimSpace(children()); text != "" {
			b.WriteString("~~" + text + "~~")
		}
	case atom.Code, atom.Kbd, atom.Samp:
		if text := strings.TrimSpace(textOf(n)); text != "" {
			fence := "`"
			for strings.Contains(text, fence) {
				fence += "`"
			}
			b.WriteString(fence + text + fence)
		}
	case atom.Script, atom.Style, atom.Noscript, atom.Template:
	default:
		b.WriteString(children())
	}
}

// escapeMarkdown escapes the characters that would start markdown syntax in
// running text.
func escapeMarkdown(s string) string {
	if !strings.ContainsAny(s, "\\*`[]") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\*`[]", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
