//go:build ignore

// compile-snippets extracts every ```go block from docs/**/*.md(x) and checks
// that it compiles against the module in the current checkout.
//
// Run from anywhere inside the repository:
//
//	go run docs/scripts/compile-snippets.go            # check docs/
//	go run docs/scripts/compile-snippets.go -v         # also list skipped blocks
//	go run docs/scripts/compile-snippets.go -fragments=strict
//
// Blocks fall into three groups:
//
//   - Complete programs (the block declares a package clause). Each one is
//     written to its own package inside the module and checked with go vet.
//     Any failure is an error and the exit status is 1.
//   - Fragments (no package clause). A fragment is wrapped in a template: if it
//     parses as top-level declarations it becomes a package with auto-added
//     imports; if it parses as statements it becomes a function body. Imports
//     are added for every package qualifier the fragment uses (std library,
//     SDK packages, and any alias used by complete programs elsewhere in the
//     docs). Fragments cannot be fully self-contained, so "undefined: name",
//     "declared and not used" and "imported and not used" are ignored. What
//     remains is reported: undefined package members (ai.Step), wrong
//     argument counts, wrong types, and syntax errors.
//   - Skipped blocks. A fence info string that contains the word skip-compile
//     (```go skip-compile) opts a block out. Use it for old-API "Before:"
//     blocks and programs that need a third-party module.
//
// Fragment findings are printed but only fail the run with
// -fragments=strict (or -fragments=members, which fails on undefined package
// members only).
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const skipMarker = "skip-compile"

type block struct {
	file   string // slash path relative to repo root
	line   int    // line number of the first code line
	info   string // fence info string after the language
	code   string
	skip   bool
	kind   string // "program", "decl", "stmt", "syntax"
	id     int
	synErr string
}

type finding struct {
	file string
	line int
	msg  string
}

var (
	verbose   = flag.Bool("v", false, "list skipped blocks and per-file counts")
	docsDir   = flag.String("docs", "docs", "docs directory, relative to the repo root")
	fragMode  = flag.String("fragments", "report", "fragment policy: report, members or strict")
	keepTmp   = flag.Bool("keep", false, "keep the temporary package directory")
	fenceRe   = regexp.MustCompile("^(\\s*)(`{3,}|~{3,})\\s*(.*)$")
	qualRe    = regexp.MustCompile(`\b([a-z][a-zA-Z0-9]*)\.[A-Za-z_]`)
	errLineRe = regexp.MustCompile(`^(?:vet: )?(/DOCS/)?(\S+?):(\d+)(?::(\d+))?: (.*)$`)
	pkgClause = regexp.MustCompile(`(?m)^package\s+\w+`)
)

var stdAliases = map[string]string{
	"context": "context", "fmt": "fmt", "log": "log", "os": "os", "time": "time",
	"http": "net/http", "json": "encoding/json", "strings": "strings", "errors": "errors",
	"io": "io", "sync": "sync", "bytes": "bytes", "httptest": "net/http/httptest",
	"filepath": "path/filepath", "signal": "os/signal", "syscall": "syscall",
	"bufio": "bufio", "strconv": "strconv", "sort": "sort", "slices": "slices",
	"base64": "encoding/base64", "url": "net/url", "slog": "log/slog", "reflect": "reflect",
	"regexp": "regexp", "exec": "os/exec", "runtime": "runtime", "math": "math", "rand": "math/rand",
	"atomic": "sync/atomic", "utf8": "unicode/utf8", "sha256": "crypto/sha256", "hex": "encoding/hex",
}

func main() {
	flag.Parse()
	root, err := moduleRoot()
	check(err)
	modPath, err := modulePath(root)
	check(err)
	check(os.Chdir(root))

	blocks, err := collect(*docsDir)
	check(err)

	aliases := buildAliases(root, modPath, blocks)
	// A name that some block uses as a variable (provider, registry, ...) is
	// ambiguous: "provider.Foo" is as likely a method call as a package member.
	// Auto-import only unambiguous aliases; a fragment can still import it.
	for _, b := range blocks {
		for n := range declaredNames(b.code) {
			if _, ok := aliases[n]; ok {
				ambiguous[n] = true
			}
		}
	}

	tmp, err := os.MkdirTemp(root, ".docsnippets-")
	check(err)
	if !*keepTmp {
		defer os.RemoveAll(tmp)
	}
	tmpRel := filepath.Base(tmp)

	var progErrs, fragErrs, synErrs, skipped []finding
	var nProg, nDecl, nStmt int

	// Classify.
	var programs, decls, stmts []*block
	for i, b := range blocks {
		b.id = i
		if b.skip {
			skipped = append(skipped, finding{b.file, b.line, "skipped"})
			continue
		}
		if pkgClause.MatchString(b.code) {
			b.kind = "program"
			programs = append(programs, b)
			continue
		}
		classifyFragment(b)
		switch b.kind {
		case "decl":
			decls = append(decls, b)
		case "stmt":
			stmts = append(stmts, b)
		default:
			synErrs = append(synErrs, finding{b.file, b.line, "syntax: " + b.synErr})
		}
	}
	nProg, nDecl, nStmt = len(programs), len(decls), len(stmts)

	byDir := map[string]*block{}

	// Complete programs.
	var progDirs []string
	for _, b := range programs {
		if msg := checkImports(root, modPath, b.code); msg != "" {
			progErrs = append(progErrs, finding{b.file, b.line, msg})
			continue
		}
		dir := filepath.Join(tmp, fmt.Sprintf("p%d", b.id))
		check(os.MkdirAll(dir, 0o755))
		src := fmt.Sprintf("//line /DOCS/%s:%d\n%s\n", b.file, b.line, stripBuildTags(b.code))
		check(os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644))
		byDir[dir] = b
		progDirs = append(progDirs, "./"+tmpRel+"/"+filepath.Base(dir))
	}
	if len(progDirs) > 0 {
		// Packages that cannot even load (missing go.sum entry, unavailable
		// module) would abort the whole vet run, so find them first.
		loadErrs, ok := loadCheck(root, tmpRel, progDirs, blocks)
		progErrs = append(progErrs, loadErrs...)
		progDirs = ok
	}
	if len(progDirs) > 0 {
		out := run(root, append([]string{"vet", "-p", "2"}, progDirs...)...)
		progErrs = append(progErrs, parseErrors(out, tmpRel, blocks, nil)...)
	}

	// Fragments: declarations, one package each.
	var declDirs []string
	var extFrags []finding
	for _, b := range decls {
		if msg := checkImports(root, modPath, "package frag\n"+b.code); msg != "" {
			extFrags = append(extFrags, finding{b.file, b.line, msg})
			continue
		}
		dir := filepath.Join(tmp, fmt.Sprintf("d%d", b.id))
		check(os.MkdirAll(dir, 0o755))
		src := wrapDecl(b, aliases)
		check(os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644))
		declDirs = append(declDirs, "./"+tmpRel+"/"+filepath.Base(dir))
	}
	// Fragments: statements, one shared package, one file per fragment.
	if len(stmts) > 0 {
		dir := filepath.Join(tmp, "stmts")
		check(os.MkdirAll(dir, 0o755))
		for _, b := range stmts {
			src := wrapStmt(b, aliases)
			check(os.WriteFile(filepath.Join(dir, fmt.Sprintf("s%d.go", b.id)), []byte(src), 0o644))
		}
		declDirs = append(declDirs, "./"+tmpRel+"/stmts")
	}
	if len(declDirs) > 0 {
		var le []finding
		le, declDirs = loadCheck(root, tmpRel, declDirs, blocks)
		extFrags = append(extFrags, le...)
	}
	if len(declDirs) > 0 {
		out := run(root, append([]string{"build", "-p", "2", "-gcflags=-e", "-o", os.DevNull}, declDirs...)...)
		fragErrs = parseErrors(out, tmpRel, blocks, fragmentNoise)
	}

	// Report.
	sortFindings(progErrs)
	sortFindings(fragErrs)
	sortFindings(synErrs)

	fmt.Printf("Checked %d go blocks in %s/: %d complete programs, %d declaration fragments, %d statement fragments, %d syntax-only failures, %d skipped (%s)\n\n",
		len(blocks), *docsDir, nProg, nDecl, nStmt, len(synErrs), len(skipped), skipMarker)

	if *verbose {
		for _, s := range skipped {
			fmt.Printf("  skipped %s:%d\n", s.file, s.line)
		}
		fmt.Println()
	}

	fmt.Printf("== Complete programs: %d error(s) ==\n", len(progErrs))
	for _, f := range progErrs {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}

	members, other := splitMembers(fragErrs)
	fmt.Printf("\n== Fragments: %d undefined package member(s) ==\n", len(members))
	for _, f := range members {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}
	fmt.Printf("\n== Fragments: %d other type error(s) (wrong args, types, methods) ==\n", len(other))
	for _, f := range other {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}
	fmt.Printf("\n== Fragments: %d block(s) importing modules outside go.mod (not checked) ==\n", len(extFrags))
	sortFindings(extFrags)
	for _, f := range extFrags {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}
	fmt.Printf("\n== Fragments: %d block(s) that do not parse (pseudo-code or syntax errors) ==\n", len(synErrs))
	for _, f := range synErrs {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}

	fail := len(progErrs) > 0
	switch *fragMode {
	case "strict":
		fail = fail || len(members)+len(other)+len(synErrs) > 0
	case "members":
		fail = fail || len(members) > 0
	}
	if !*keepTmp {
		os.RemoveAll(tmp) // os.Exit skips deferred calls
	}
	if fail {
		fmt.Println("\nFAIL")
		os.Exit(1)
	}
	fmt.Println("\nOK")
}

// ---- collection ----

func collect(docs string) ([]*block, error) {
	var out []*block
	err := filepath.WalkDir(docs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Templates hold [placeholders]; scripts/ holds tool docs.
			if d.Name() == "node_modules" || (d.Name() == "_templates" && filepath.Dir(p) == docs) {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(p); ext != ".md" && ext != ".mdx" {
			return nil
		}
		bs, err := extractBlocks(p)
		out = append(out, bs...)
		return err
	})
	return out, err
}

func extractBlocks(path string) ([]*block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []*block
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var cur *block
	var fenceCh byte
	var fenceLen int
	var indent string
	var lines []string
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		m := fenceRe.FindStringSubmatch(line)
		if cur == nil {
			if m == nil {
				continue
			}
			fenceCh, fenceLen, indent = m[2][0], len(m[2]), m[1]
			info := strings.TrimSpace(m[3])
			lang := info
			rest := ""
			if i := strings.IndexAny(info, " \t{"); i >= 0 {
				lang, rest = info[:i], strings.TrimSpace(info[i:])
			}
			if lang == "go" || lang == "golang" {
				cur = &block{file: filepath.ToSlash(path), line: n + 1, info: rest}
				cur.skip = strings.Contains(rest, skipMarker)
			} else {
				cur = &block{} // non-Go fence: consume and discard
				cur.file = ""
			}
			lines = lines[:0]
			continue
		}
		if m != nil && m[2][0] == fenceCh && len(m[2]) >= fenceLen && strings.TrimSpace(m[3]) == "" {
			if cur.file != "" {
				cur.code = strings.Join(lines, "\n")
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		lines = append(lines, strings.TrimPrefix(line, indent))
	}
	return out, sc.Err()
}

// ---- aliases and imports ----

func buildAliases(root, modPath string, blocks []*block) map[string]string {
	al := map[string]string{}
	for k, v := range stdAliases {
		al[k] = v
	}
	// Every SDK package by last path element.
	_ = filepath.WalkDir(filepath.Join(root, "pkg"), func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.Contains(p, "/internal") || strings.Contains(p, "/testdata") {
			return filepath.SkipDir
		}
		ents, _ := os.ReadDir(p)
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				rel, _ := filepath.Rel(root, p)
				name := filepath.Base(p)
				if _, ok := al[name]; !ok {
					al[name] = modPath + "/" + filepath.ToSlash(rel)
				}
				break
			}
		}
		return nil
	})
	// Aliases used by programs in the docs win over the guesses above.
	for _, b := range blocks {
		if !pkgClause.MatchString(b.code) {
			continue
		}
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, "x.go", b.code, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			if !strings.HasPrefix(path, modPath+"/") {
				continue
			}
			name := filepath.Base(path)
			if im.Name != nil {
				name = im.Name.Name
			}
			if name == "_" || name == "." {
				continue
			}
			al[name] = path
		}
	}
	return al
}

// checkImports returns a message when a program imports something the module
// cannot resolve, so one bad block does not abort the whole vet run.
func checkImports(root, modPath, code string) string {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "x.go", code, parser.ImportsOnly)
	if err != nil {
		return "syntax error: " + firstLine(err.Error())
	}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		first := strings.SplitN(path, "/", 2)[0]
		switch {
		case !strings.Contains(first, "."):
			// std library
		case strings.HasPrefix(path, modPath+"/"):
			rel := strings.TrimPrefix(path, modPath+"/")
			if st, err := os.Stat(filepath.Join(root, rel)); err != nil || !st.IsDir() {
				return "import " + strconv.Quote(path) + ": package not found in this module"
			}
		case path == modPath:
		default:
			if !inGoMod(root, path) {
				return "import " + strconv.Quote(path) + ": not a dependency of this module (add `" + skipMarker + "` to the fence if intentional)"
			}
		}
	}
	return ""
}

var goModCache string

func inGoMod(root, path string) bool {
	if goModCache == "" {
		b, _ := os.ReadFile(filepath.Join(root, "go.mod"))
		goModCache = string(b)
	}
	for _, line := range strings.Split(goModCache, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			mod := fields[0]
			if mod == "require" && len(fields) > 1 {
				mod = fields[1]
			}
			if strings.Contains(mod, ".") && (path == mod || strings.HasPrefix(path, mod+"/")) {
				return true
			}
		}
	}
	return false
}

// ---- fragments ----

func classifyFragment(b *block) {
	fs := token.NewFileSet()
	if _, err := parser.ParseFile(fs, "x.go", "package p\n"+b.code, parser.AllErrors); err == nil {
		b.kind = "decl"
		return
	}
	fs = token.NewFileSet()
	_, err := parser.ParseFile(fs, "x.go", "package p\nfunc _() {\n"+b.code+"\n}\n", parser.AllErrors)
	if err == nil {
		b.kind = "stmt"
		return
	}
	b.kind = "syntax"
	b.synErr = firstLine(err.Error())
}

func ownImports(code string) map[string]bool {
	have := map[string]bool{}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "x.go", "package p\n"+code, parser.ImportsOnly)
	if err != nil || f == nil {
		return have
	}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := filepath.Base(path)
		if im.Name != nil {
			name = im.Name.Name
		}
		have[name] = true
	}
	return have
}

// declared returns names declared in the fragment itself (variables, funcs)
// that would shadow a package alias.
var declRe = regexp.MustCompile(`(?m)(?:\b([a-z][A-Za-z0-9]*)\s*(?:,\s*[a-z_][A-Za-z0-9]*\s*)*:?=[^=]|\bvar\s+([a-z][A-Za-z0-9]*)|\bfunc\s*\([^)]*\b([a-z][A-Za-z0-9]*)\s+[\*\w]|[(,]\s*([a-z][A-Za-z0-9]*)\s+[\*\[\w.]+[,)])`)

func declaredNames(code string) map[string]bool {
	d := map[string]bool{}
	for _, m := range declRe.FindAllStringSubmatch(code, -1) {
		for _, n := range m[1:] {
			if n != "" {
				d[n] = true
			}
		}
	}
	return d
}

var ambiguous = map[string]bool{}

func importBlock(b *block, aliases map[string]string, skipImports bool) string {
	if skipImports {
		return ""
	}
	have := ownImports(b.code)
	declared := declaredNames(b.code)
	used := map[string]bool{}
	for _, m := range qualRe.FindAllStringSubmatch(b.code, -1) {
		used[m[1]] = true
	}
	var names []string
	for n := range used {
		if _, ok := aliases[n]; ok && !have[n] && !declared[n] && !ambiguous[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		fmt.Fprintf(&sb, "import %s %q\n", n, aliases[n])
	}
	return sb.String()
}

func wrapDecl(b *block, aliases map[string]string) string {
	imp := importBlock(b, aliases, false)
	// Imports must precede declarations: if the fragment has its own import
	// declarations, they still come first in the fragment text, and extra
	// import declarations before them are legal.
	return "package frag\n" + imp + fmt.Sprintf("//line /DOCS/%s:%d\n%s\n", b.file, b.line, b.code)
}

func wrapStmt(b *block, aliases map[string]string) string {
	imp := importBlock(b, aliases, false)
	fn := fmt.Sprintf("snippet%d", b.id)
	return "package stmts\n" + imp + "func " + fn + "() {\n" + fmt.Sprintf("//line /DOCS/%s:%d\n%s\n", b.file, b.line, b.code) + "}\n"
}

var noiseRe = regexp.MustCompile(`declared and not used|imported and not used|^undefined: [A-Za-z_][A-Za-z0-9_]*$|missing return|is not used$|no new variables|redeclared|too many return values|not enough return values|other declaration of|already defined|not in selector|imported as .* and not used|is not in a loop|missing function body|^label .* defined and not used|too many errors|^missing function body|^cannot use _ as value|^invalid operation: operator . not defined on`)

func fragmentNoise(msg string) bool { return noiseRe.MatchString(msg) }

var memberRe = regexp.MustCompile(`^undefined: [a-z][A-Za-z0-9]*\.[A-Za-z_]|has no field or method|^undefined \(type|\(undefined\)$|^.* undefined \(type`)

func splitMembers(fs []finding) (members, other []finding) {
	for _, f := range fs {
		if strings.HasPrefix(f.msg, "undefined: ") && strings.Contains(f.msg, ".") {
			members = append(members, f)
		} else {
			other = append(other, f)
		}
	}
	return
}

// ---- running the toolchain ----

var buildTagRe = regexp.MustCompile(`(?m)^//(go:build|\s*\+build)[^\n]*\n?`)

func stripBuildTags(code string) string { return buildTagRe.ReplaceAllString(code, "") }

// loadCheck runs go list over the program packages and returns the ones that
// fail to load as findings, plus the directories that are fine.
func loadCheck(root, tmpRel string, dirs []string, blocks []*block) ([]finding, []string) {
	args := []string{"list", "-e", "-deps", "-f",
		"{{if .Error}}ERR\t{{.ImportPath}}\t{{.Error}}{{end}}{{range .DepsErrors}}ERR\t{{$.ImportPath}}\t{{.Err}}\n{{end}}"}
	out := run(root, append(args, dirs...)...)
	bad := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) == 3 && parts[0] == "ERR" && strings.Contains(parts[1], tmpRel) {
			if _, seen := bad[parts[1]]; !seen {
				bad[parts[1]] = strings.ReplaceAll(firstLine(parts[2]), "/DOCS/", "")
			}
		}
	}
	var res []finding
	var ok []string
	for _, d := range dirs {
		var msg string
		for ip, m := range bad {
			if strings.HasSuffix(ip, "/"+tmpRel+"/"+filepath.Base(d)) {
				msg = m
			}
		}
		if msg == "" {
			ok = append(ok, d)
			continue
		}
		id, _ := strconv.Atoi(strings.TrimLeft(filepath.Base(d), "pd"))
		res = append(res, finding{blocks[id].file, blocks[id].line, "cannot load: " + msg})
	}
	return res, ok
}

func run(root string, args ...string) string {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	return buf.String()
}

func parseErrors(out, tmpRel string, blocks []*block, noise func(string) bool) []finding {
	var res []finding
	seen := map[string]bool{}
	dirRe := regexp.MustCompile(regexp.QuoteMeta(tmpRel) + `/(p|d|stmts)(\d*)`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var f finding
		if m := errLineRe.FindStringSubmatch(line); m != nil && (m[1] != "" || strings.Contains(m[2], "/DOCS/") || strings.HasSuffix(m[2], ".md") || strings.HasSuffix(m[2], ".mdx")) {
			file := m[2]
			if i := strings.Index(file, "/DOCS/"); i >= 0 {
				file = file[i+len("/DOCS/"):]
			}
			// vet joins the //line path onto the package dir.
			if i := strings.Index(file, tmpRel+"/"); i >= 0 {
				if j := strings.Index(file[i:], "/docs/"); j >= 0 {
					file = file[i+j+1:]
				}
			}
			ln, _ := strconv.Atoi(m[3])
			f = finding{file, ln, m[5]}
		} else if m := dirRe.FindStringSubmatch(line); m != nil {
			// Package-level failure that carries no //line position.
			if id, err := strconv.Atoi(m[2]); err == nil && id < len(blocks) {
				f = finding{blocks[id].file, blocks[id].line, line}
			} else {
				f = finding{"(unmapped)", 0, line}
			}
		} else {
			continue
		}
		f.msg = strings.TrimPrefix(f.msg, "vet: ")
		if noise != nil && noise(f.msg) {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", f.file, f.line, f.msg)
		if seen[key] {
			continue
		}
		seen[key] = true
		res = append(res, f)
	}
	return res
}

// ---- helpers ----

func sortFindings(fs []finding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].file != fs[j].file {
			return fs[i].file < fs[j].file
		}
		return fs[i].line < fs[j].line
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func modulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(l, "module ")), nil
		}
	}
	return "", fmt.Errorf("no module line in go.mod")
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "compile-snippets:", err)
		os.Exit(2)
	}
}

var _ = ast.Inspect
