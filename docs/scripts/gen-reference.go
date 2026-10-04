//go:build ignore

// gen-reference regenerates Go struct field tables and constant lists inside
// the reference pages under docs/ from the Go source in pkg/.
//
// A generated section sits between two marker comments:
//
//	{/* gen:fields ai.GenerateTextOptions */}
//	...generated table...
//	{/* /gen:fields */}
//
//	{/* gen:consts ai.ToolUIPartState */}
//	...generated table...
//	{/* /gen:consts */}
//
// Everything outside the markers is hand-written and left untouched, so
// running the generator twice produces no changes.
//
// The reference is "<package>.<Type>". <package> is a package name
// ("ai", "openai") or a directory path below pkg/ ("providers/openai") when the
// bare name is ambiguous.
//
// Usage (from the repository root):
//
//	go run docs/scripts/gen-reference.go            # rewrite the pages
//	go run docs/scripts/gen-reference.go --check    # exit 1 if any page is out of date
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markerRe = regexp.MustCompile(`(?m)^\{/\* gen:(fields|consts) ([A-Za-z0-9_/.\-]+) \*/\}[ \t]*\n`)

type generator struct {
	pkgDirs map[string][]string // package name or relative dir -> directories
	docs    map[string]*doc.Package
}

func main() {
	check := flag.Bool("check", false, "report out-of-date pages and exit 1 instead of rewriting them")
	docsDir := flag.String("docs", "docs", "docs directory")
	pkgDir := flag.String("pkg", "pkg", "Go package root")
	flag.Parse()

	g := &generator{pkgDirs: map[string][]string{}, docs: map[string]*doc.Package{}}
	if err := g.indexPackages(*pkgDir); err != nil {
		fatal(err)
	}

	var stale []string
	err := filepath.Walk(*docsDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !(strings.HasSuffix(p, ".mdx") || strings.HasSuffix(p, ".md")) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !markerRe.Match(src) {
			return nil
		}
		out, err := g.rewrite(string(src))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if out == string(src) {
			return nil
		}
		stale = append(stale, p)
		if *check {
			return nil
		}
		return os.WriteFile(p, []byte(out), info.Mode())
	})
	if err != nil {
		fatal(err)
	}
	if *check && len(stale) > 0 {
		fmt.Fprintln(os.Stderr, "generated reference sections are out of date; run: go run docs/scripts/gen-reference.go")
		for _, s := range stale {
			fmt.Fprintln(os.Stderr, "  "+s)
		}
		os.Exit(1)
	}
	for _, s := range stale {
		fmt.Println("updated", s)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-reference:", err)
	os.Exit(2)
}

// indexPackages records every directory under root that holds non-test Go
// files, keyed by package name and by relative directory path.
func (g *generator) indexPackages(root string) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		entries, _ := os.ReadDir(p)
		has := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				has = true
			}
		}
		if !has {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		g.pkgDirs[rel] = append(g.pkgDirs[rel], p)
		base := filepath.Base(p)
		if base != rel {
			g.pkgDirs[base] = append(g.pkgDirs[base], p)
		}
		return nil
	})
}

func (g *generator) load(spec string) (*doc.Package, error) {
	dirs := g.pkgDirs[spec]
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no package %q under pkg/", spec)
	}
	if len(dirs) > 1 {
		return nil, fmt.Errorf("package %q is ambiguous (%s); use a path such as providers/%s", spec, strings.Join(dirs, ", "), spec)
	}
	dir := dirs[0]
	if d, ok := g.docs[dir]; ok {
		return d, nil
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for name, pkg := range pkgs {
		if strings.HasSuffix(name, "_test") {
			continue
		}
		d := doc.New(pkg, dir, doc.AllDecls)
		g.docs[dir] = d
		return d, nil
	}
	return nil, fmt.Errorf("no Go package in %s", dir)
}

// fencedRanges returns the [start, end) byte ranges of fenced code blocks in
// src, so markers shown as examples inside code are left alone.
func fencedRanges(src string) [][2]int {
	var ranges [][2]int
	start, offset := -1, 0
	for _, line := range strings.SplitAfter(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if start < 0 {
				start = offset
			} else {
				ranges = append(ranges, [2]int{start, offset + len(line)})
				start = -1
			}
		}
		offset += len(line)
	}
	if start >= 0 {
		ranges = append(ranges, [2]int{start, len(src)})
	}
	return ranges
}

func inRanges(pos int, ranges [][2]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// rewrite replaces every generated section in src. Markers inside fenced
// code blocks are documentation examples and are not expanded.
func (g *generator) rewrite(src string) (string, error) {
	var out strings.Builder
	fences := fencedRanges(src)
	rest := src
	for {
		loc := markerRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			out.WriteString(rest)
			return out.String(), nil
		}
		if inRanges(len(src)-len(rest)+loc[0], fences) {
			out.WriteString(rest[:loc[1]])
			rest = rest[loc[1]:]
			continue
		}
		kind := rest[loc[2]:loc[3]]
		ref := rest[loc[4]:loc[5]]
		out.WriteString(rest[:loc[1]])
		after := rest[loc[1]:]
		endMarker := "{/* /gen:" + kind + " */}"
		end := strings.Index(after, endMarker)
		if end < 0 {
			return "", fmt.Errorf("missing %s for %s", endMarker, ref)
		}
		body, err := g.render(kind, ref)
		if err != nil {
			return "", err
		}
		out.WriteString(body)
		out.WriteString(endMarker)
		rest = after[end+len(endMarker):]
	}
}

func (g *generator) render(kind, ref string) (string, error) {
	i := strings.LastIndex(ref, ".")
	if i < 0 {
		return "", fmt.Errorf("bad reference %q, want <package>.<Type>", ref)
	}
	pkg, err := g.load(ref[:i])
	if err != nil {
		return "", err
	}
	var typ *doc.Type
	for _, t := range pkg.Types {
		if t.Name == ref[i+1:] {
			typ = t
		}
	}
	if typ == nil {
		return "", fmt.Errorf("type %q not found", ref)
	}
	if kind == "consts" {
		return renderConsts(typ, ref)
	}
	return renderFields(typ, ref)
}

func renderFields(typ *doc.Type, ref string) (string, error) {
	var st *ast.StructType
	for _, s := range typ.Decl.Specs {
		if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == typ.Name {
			st, _ = ts.Type.(*ast.StructType)
		}
	}
	if st == nil {
		return "", fmt.Errorf("%s is not a struct", ref)
	}
	var b strings.Builder
	b.WriteString("\n| Field | Type | Description |\n| --- | --- | --- |\n")
	rows := 0
	for _, f := range st.Fields.List {
		desc := fieldDoc(f)
		typeStr := "`" + escapeCode(exprString(f.Type)) + "`"
		if len(f.Names) == 0 {
			if !isExportedEmbedded(f.Type) {
				continue
			}
			if desc == "" {
				desc = "Embedded."
			}
			fmt.Fprintf(&b, "| (embedded) | %s | %s |\n", typeStr, desc)
			rows++
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", n.Name, typeStr, desc)
			rows++
		}
	}
	if rows == 0 {
		return "\nThis type has no exported fields.\n\n", nil
	}
	b.WriteString("\n")
	return b.String(), nil
}

func renderConsts(typ *doc.Type, ref string) (string, error) {
	var b strings.Builder
	b.WriteString("\n| Constant | Value | Description |\n| --- | --- | --- |\n")
	rows := 0
	for _, c := range typ.Consts {
		for _, s := range c.Decl.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if !n.IsExported() {
					continue
				}
				val := ""
				if i < len(vs.Values) {
					val = exprString(vs.Values[i])
				}
				desc := cleanDoc(vs.Doc.Text())
				if desc == "" && vs.Comment != nil {
					desc = cleanDoc(vs.Comment.Text())
				}
				fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", n.Name, escapeCode(val), desc)
				rows++
			}
		}
	}
	if rows == 0 {
		return "", fmt.Errorf("%s has no exported constants", ref)
	}
	b.WriteString("\n")
	return b.String(), nil
}

func fieldDoc(f *ast.Field) string {
	d := cleanDoc(f.Doc.Text())
	if d == "" && f.Comment != nil {
		d = cleanDoc(f.Comment.Text())
	}
	return d
}

func isExportedEmbedded(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return isExportedEmbedded(t.X)
	case *ast.SelectorExpr:
		return t.Sel.IsExported()
	case *ast.Ident:
		return t.IsExported()
	}
	return true
}

func exprString(e ast.Expr) string {
	var b bytes.Buffer
	writeExpr(&b, e)
	return b.String()
}

// writeExpr prints a type expression on one line.
func writeExpr(b *bytes.Buffer, e ast.Expr) {
	switch t := e.(type) {
	case *ast.Ident:
		b.WriteString(t.Name)
	case *ast.BasicLit:
		b.WriteString(t.Value)
	case *ast.StarExpr:
		b.WriteByte('*')
		writeExpr(b, t.X)
	case *ast.SelectorExpr:
		writeExpr(b, t.X)
		b.WriteByte('.')
		b.WriteString(t.Sel.Name)
	case *ast.ArrayType:
		b.WriteByte('[')
		if t.Len != nil {
			writeExpr(b, t.Len)
		}
		b.WriteByte(']')
		writeExpr(b, t.Elt)
	case *ast.MapType:
		b.WriteString("map[")
		writeExpr(b, t.Key)
		b.WriteByte(']')
		writeExpr(b, t.Value)
	case *ast.ChanType:
		switch t.Dir {
		case ast.RECV:
			b.WriteString("<-chan ")
		case ast.SEND:
			b.WriteString("chan<- ")
		default:
			b.WriteString("chan ")
		}
		writeExpr(b, t.Value)
	case *ast.FuncType:
		b.WriteString("func")
		writeParams(b, t.Params)
		if t.Results != nil && len(t.Results.List) > 0 {
			b.WriteByte(' ')
			if len(t.Results.List) == 1 && len(t.Results.List[0].Names) == 0 {
				writeExpr(b, t.Results.List[0].Type)
			} else {
				writeParams(b, t.Results)
			}
		}
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			b.WriteString("interface{}")
		} else {
			b.WriteString("interface{...}")
		}
	case *ast.StructType:
		b.WriteString("struct{...}")
	case *ast.Ellipsis:
		b.WriteString("...")
		writeExpr(b, t.Elt)
	case *ast.ParenExpr:
		b.WriteByte('(')
		writeExpr(b, t.X)
		b.WriteByte(')')
	case *ast.IndexExpr:
		writeExpr(b, t.X)
		b.WriteByte('[')
		writeExpr(b, t.Index)
		b.WriteByte(']')
	case *ast.IndexListExpr:
		writeExpr(b, t.X)
		b.WriteByte('[')
		for i, x := range t.Indices {
			if i > 0 {
				b.WriteString(", ")
			}
			writeExpr(b, x)
		}
		b.WriteByte(']')
	case *ast.BinaryExpr:
		writeExpr(b, t.X)
		b.WriteString(" " + t.Op.String() + " ")
		writeExpr(b, t.Y)
	case *ast.UnaryExpr:
		b.WriteString(t.Op.String())
		writeExpr(b, t.X)
	case *ast.CallExpr:
		writeExpr(b, t.Fun)
		b.WriteByte('(')
		for i, x := range t.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			writeExpr(b, x)
		}
		b.WriteByte(')')
	default:
		b.WriteString("?")
	}
}

func writeParams(b *bytes.Buffer, fl *ast.FieldList) {
	b.WriteByte('(')
	if fl != nil {
		first := true
		for _, f := range fl.List {
			if len(f.Names) == 0 {
				if !first {
					b.WriteString(", ")
				}
				writeExpr(b, f.Type)
				first = false
				continue
			}
			for _, n := range f.Names {
				if !first {
					b.WriteString(", ")
				}
				b.WriteString(n.Name + " ")
				writeExpr(b, f.Type)
				first = false
			}
		}
	}
	b.WriteByte(')')
}

// cleanDoc flattens a doc comment to one table-safe line.
func cleanDoc(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	return escapeProse(s)
}

func escapeCode(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// escapeProse makes text safe for an MDX table cell: pipes, braces and angle
// brackets outside inline code are escaped. Text inside backticks keeps its
// braces (MDX treats inline code literally) but pipes are still escaped.
func escapeProse(s string) string {
	parts := strings.Split(s, "`")
	for i, p := range parts {
		p = strings.ReplaceAll(p, "|", "\\|")
		if i%2 == 0 {
			p = strings.NewReplacer("{", "\\{", "}", "\\}", "<", "&lt;", ">", "&gt;", "*", "\\*").Replace(p)
		}
		parts[i] = p
	}
	return strings.Join(parts, "`")
}
