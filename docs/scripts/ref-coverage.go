//go:build ignore

// ref-coverage reports how many exported top-level identifiers (types,
// functions, constants, variables) in selected packages appear anywhere in the
// docs. Identifiers listed in the allowlist file are excluded from the count.
//
// Usage (from the repository root):
//
//	go run docs/scripts/ref-coverage.go
//	go run docs/scripts/ref-coverage.go -min pkg/ai=90,pkg/agent=90   # fail below threshold
//	go run docs/scripts/ref-coverage.go -list pkg/mcp                 # list missing identifiers
//
// Allowlist format (docs/scripts/ref-coverage-allowlist.txt): one entry per
// line, "<package-dir> <Identifier> # reason". Blank lines and lines starting
// with # are ignored. A reason is required.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// wordRe matches Go identifier-shaped tokens. An identifier counts as
// documented when it appears as a whole token anywhere in the docs.
var wordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func main() {
	pkgsFlag := flag.String("pkgs", "pkg/ai,pkg/agent,pkg/mcp,pkg/harness,pkg/workflow", "comma-separated package directories")
	docsDir := flag.String("docs", "docs", "docs directory")
	allowFile := flag.String("allowlist", "docs/scripts/ref-coverage-allowlist.txt", "allowlist file")
	minFlag := flag.String("min", "", "comma-separated pkg=percent thresholds; exit 1 if any is not met")
	list := flag.String("list", "", "package directory whose missing identifiers should be listed")
	flag.Parse()

	corpus, err := readDocs(*docsDir)
	if err != nil {
		fatal(err)
	}
	words := map[string]bool{}
	for _, w := range wordRe.FindAllString(corpus, -1) {
		words[w] = true
	}
	allow, err := readAllowlist(*allowFile)
	if err != nil {
		fatal(err)
	}
	mins := map[string]float64{}
	if *minFlag != "" {
		for _, kv := range strings.Split(*minFlag, ",") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				fatal(fmt.Errorf("bad -min entry %q", kv))
			}
			v, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				fatal(err)
			}
			mins[parts[0]] = v
		}
	}

	failed := false
	fmt.Printf("%-14s %8s %8s %8s %8s\n", "package", "total", "allowed", "missing", "covered")
	for _, dir := range strings.Split(*pkgsFlag, ",") {
		ids, err := exported(dir)
		if err != nil {
			fatal(err)
		}
		total, covered := 0, 0
		var missing []string
		allowed := 0
		for _, id := range ids {
			if allow[dir+" "+id] {
				allowed++
				continue
			}
			total++
			if words[id] {
				covered++
			} else {
				missing = append(missing, id)
			}
		}
		pct := 100.0
		if total > 0 {
			pct = 100 * float64(covered) / float64(total)
		}
		fmt.Printf("%-14s %8d %8d %8d %7.1f%%\n", dir, total, allowed, len(missing), pct)
		if *list == dir {
			for _, m := range missing {
				fmt.Println("  missing:", m)
			}
		}
		if min, ok := mins[dir]; ok && pct < min {
			fmt.Printf("  FAIL: %s is below %.0f%%\n", dir, min)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ref-coverage:", err)
	os.Exit(2)
}

// readDocs concatenates every .md and .mdx file under dir.
func readDocs(dir string) (string, error) {
	var sb strings.Builder
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !(strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx")) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sb.Write(b)
		sb.WriteByte('\n')
		return nil
	})
	return sb.String(), err
}

func readAllowlist(path string) (map[string]bool, error) {
	out := map[string]bool{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entry, reason, ok := strings.Cut(line, "#")
		if !ok || strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("%s:%d: allowlist entry needs a '# reason'", path, i+1)
		}
		f := strings.Fields(entry)
		if len(f) != 2 {
			return nil, fmt.Errorf("%s:%d: want '<package-dir> <Identifier> # reason'", path, i+1)
		}
		out[f[0]+" "+f[1]] = true
	}
	return out, nil
}

// exported returns the sorted exported top-level identifiers of the package in dir.
func exported(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil && d.Name.IsExported() {
						seen[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								seen[s.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n.IsExported() {
									seen[n.Name] = true
								}
							}
						}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}
