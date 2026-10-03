// Package docs builds, loads and searches the Go AI SDK documentation bundle
// served by goai-docs-mcp.
package docs

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SiteURL is where the docs are published.
const SiteURL = "https://goaisdk.com"

// Doc is one documentation page.
type Doc struct {
	// Path is the page's URL path on the site, such as /docs/agents/building-agents.
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Section is the top-level docs directory without its numeric prefix.
	Section string `json:"section"`
	// Type is guide, reference, provider, migration or troubleshooting (or recipe).
	Type string `json:"type"`
	// Body is the page as markdown, without front matter.
	Body string `json:"body"`
}

// URL returns the markdown URL of the page on the site.
func (d Doc) URL() string { return SiteURL + d.Path + ".md" }

var (
	numPrefix = regexp.MustCompile(`^\d+-`)
	noteRe    = regexp.MustCompile(`(?s)<Note(?:\s+type="(\w+)")?\s*>\s*(.*?)\s*</Note>`)
	importRe  = regexp.MustCompile(`(?m)^\s*(import|export)\s.+$\n?`)
	leadingH1 = regexp.MustCompile(`^#[ \t]+[^\n]*\n+`)
)

type frontMatter struct {
	ID          string `yaml:"id"`
	Slug        string `yaml:"slug"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Draft       bool   `yaml:"draft"`
	Unlisted    bool   `yaml:"unlisted"`
}

// excludedTop lists top-level docs entries that are not user documentation.
// Everything else is served, so new directories are picked up automatically.
var excludedTop = map[string]bool{
	"scripts":                          true,
	"_templates":                       true,
	"README.md":                        true,
	"CONTRIBUTING_DOCS.md":             true,
	"DOCUMENTATION_STYLE_GUIDE.md":     true,
	"QUALITY_TOOLS_QUICK_REFERENCE.md": true,
}

// Excluded reports whether a top-level docs entry is left out of the bundle.
func Excluded(name string) bool { return excludedTop[name] }

// Build reads every markdown page in fsys (rooted at the docs directory) and
// returns the pages sorted by path. It mirrors how the Docusaurus site
// derives URLs: numeric directory prefixes are dropped, index pages take
// their directory's URL, and a slug in the front matter overrides the file
// name. A page that fails to parse is an error.
func Build(fsys fs.FS) ([]Doc, error) {
	var out []Doc
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != "." && !strings.Contains(p, "/") && excludedTop[name] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(name, "_") || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		ext := path.Ext(name)
		if (ext != ".md" && ext != ".mdx") || strings.HasPrefix(name, "_") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		doc, ok, err := parsePage(p, string(raw))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if ok {
			out = append(out, doc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func splitFrontMatter(s string) (string, string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", s
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", s
	}
	fm := s[4 : 4+end]
	rest := s[4+end+4:]
	return fm, strings.TrimPrefix(rest, "\n")
}

func parsePage(rel, raw string) (Doc, bool, error) {
	fmText, body := splitFrontMatter(raw)
	var fm frontMatter
	if fmText != "" {
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return Doc{}, false, fmt.Errorf("front matter: %w", err)
		}
	}
	if fm.Draft || fm.Unlisted {
		return Doc{}, false, nil
	}
	segs := strings.Split(rel, "/")
	file := strings.TrimSuffix(segs[len(segs)-1], path.Ext(segs[len(segs)-1]))
	dirs := make([]string, 0, len(segs)-1)
	for _, s := range segs[:len(segs)-1] {
		dirs = append(dirs, numPrefix.ReplaceAllString(s, ""))
	}
	var urlPath string
	switch {
	case strings.HasPrefix(fm.Slug, "/"):
		urlPath = "/docs" + strings.TrimSuffix(fm.Slug, "/")
	case fm.Slug != "":
		urlPath = "/docs/" + strings.Join(append(dirs, strings.Trim(fm.Slug, "/")), "/")
	case file == "index":
		urlPath = "/docs/" + strings.Join(dirs, "/")
	default:
		urlPath = "/docs/" + strings.Join(append(dirs, numPrefix.ReplaceAllString(file, "")), "/")
	}
	urlPath = strings.TrimSuffix(urlPath, "/")

	body = cleanBody(body)
	title := fm.Title
	if title == "" {
		if m := regexp.MustCompile(`(?m)^#\s+(.+)$`).FindStringSubmatch(body); m != nil {
			title = strings.TrimSpace(m[1])
		} else {
			title = file
		}
	}
	body = leadingH1.ReplaceAllString(strings.TrimSpace(body), "")

	section := ""
	if len(segs) > 1 {
		section = dirs[0]
	}
	d := Doc{
		Path:        urlPath,
		Title:       title,
		Description: strings.TrimSpace(fm.Description),
		Section:     section,
		Type:        pageType(dirs, numPrefix.ReplaceAllString(file, "")),
		Body:        strings.TrimSpace(body),
	}
	return d, true, nil
}

// cleanBody flattens MDX-only syntax outside fenced code blocks.
func cleanBody(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	inFence := false
	var prose strings.Builder
	flush := func() {
		t := prose.String()
		prose.Reset()
		t = importRe.ReplaceAllString(t, "")
		t = noteRe.ReplaceAllStringFunc(t, func(m string) string {
			sub := noteRe.FindStringSubmatch(m)
			label := "Note"
			if sub[1] != "" {
				label = strings.ToUpper(sub[1][:1]) + sub[1][1:]
			}
			quoted := strings.Split("**"+label+":** "+strings.TrimSpace(sub[2]), "\n")
			for i, l := range quoted {
				quoted[i] = strings.TrimRight("> "+strings.TrimSpace(l), " ")
			}
			return strings.Join(quoted, "\n")
		})
		b.WriteString(t)
	}
	for _, l := range lines {
		isFence := strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~")
		if isFence {
			if !inFence {
				flush()
			}
			inFence = !inFence
			b.WriteString(l + "\n")
			continue
		}
		if inFence {
			b.WriteString(l + "\n")
		} else {
			prose.WriteString(l + "\n")
		}
	}
	flush()
	return b.String()
}

func pageType(dirs []string, file string) string {
	in := func(re string) bool {
		r := regexp.MustCompile(re)
		for _, d := range dirs {
			if r.MatchString(d) {
				return true
			}
		}
		return false
	}
	switch {
	case in(`^providers?$`):
		return "provider"
	case in(`^reference$`):
		return "reference"
	case in(`^migration`) || strings.Contains(file, "migration"):
		return "migration"
	case in(`^troubleshooting$`):
		return "troubleshooting"
	case in(`^(recipes?|cookbook)$`):
		return "recipe"
	}
	return "guide"
}
