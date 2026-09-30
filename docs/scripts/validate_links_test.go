package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripNumberedPrefix(t *testing.T) {
	cases := map[string]string{
		"05-providers":       "providers",
		"39-gateway.mdx":     "gateway.mdx",
		"01-overview":        "overview",
		"index":              "index",
		"index.mdx":          "index.mdx",
		"no-prefix-here.mdx": "no-prefix-here.mdx",
		"07-reference":       "reference",
		"1-installation.mdx": "installation.mdx",
		"agent-callbacks.md": "agent-callbacks.md",
	}
	for in, want := range cases {
		if got := stripNumberedPrefix(in); got != want {
			t.Errorf("stripNumberedPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestComputeDefaultRoute(t *testing.T) {
	cases := map[string]string{
		"02-getting-started/index.mdx":                   "getting-started",
		"03-ai-sdk-core/38-video-generation.mdx":         "ai-sdk-core/video-generation",
		"05-providers/index.mdx":                         "providers",
		"05-providers/01-overview.mdx":                   "providers/overview",
		"07-reference/ai/generate-text.mdx":              "reference/ai/generate-text",
		"07-reference/types/usage.mdx":                   "reference/types/usage",
		"06-advanced/index.mdx":                          "advanced",
		"09-troubleshooting/04-context-cancellation.mdx": "troubleshooting/context-cancellation",
	}
	for in, want := range cases {
		if got := computeDefaultRoute(filepath.FromSlash(in)); got != want {
			t.Errorf("computeDefaultRoute(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteForFile_SlugOverride(t *testing.T) {
	// docs/04-advanced/index.mdx has frontmatter `slug: /advanced-guides`, which
	// must fully replace the default computed route ("advanced") so that it
	// doesn't collide with docs/06-advanced/index.mdx (whose default route is
	// also "advanced").
	relPath := filepath.FromSlash("04-advanced/index.mdx")
	got := routeForFile(relPath, "/advanced-guides", "advanced-guides")
	if got != "advanced-guides" {
		t.Errorf("routeForFile with absolute slug override = %q, want %q", got, "advanced-guides")
	}

	// Without an override, it would collide with 06-advanced's default route.
	def := computeDefaultRoute(relPath)
	other := computeDefaultRoute(filepath.FromSlash("06-advanced/index.mdx"))
	if def != other {
		t.Fatalf("test setup assumption broken: expected default routes to collide, got %q vs %q", def, other)
	}
}

func TestRouteForFile_RelativeSlugOverride(t *testing.T) {
	relPath := filepath.FromSlash("05-providers/01-overview.mdx")
	got := routeForFile(relPath, "custom-slug", "")
	if got != "providers/custom-slug" {
		t.Errorf("routeForFile with relative slug override = %q, want %q", got, "providers/custom-slug")
	}
}

func TestStripInlineCode(t *testing.T) {
	in := "See `ai.ObjectOutput[T](ai.ObjectOutputOptions{Schema: recipeSchema})` for details."
	out := stripInlineCode(in)
	if got, want := len(out), len(in); got != want {
		t.Fatalf("stripInlineCode changed line length: got %d want %d", got, want)
	}
	if regexpContainsLink(out) {
		t.Errorf("stripInlineCode left link-like syntax in place: %q", out)
	}
}

func regexpContainsLink(s string) bool {
	return markdownLinkRegex.MatchString(s)
}

func TestFenceInfo(t *testing.T) {
	if ch, length, ok := fenceInfo("```go"); !ok || ch != '`' || length != 3 {
		t.Errorf("fenceInfo(```go) = %q %d %v, want '`' 3 true", ch, length, ok)
	}
	if ch, length, ok := fenceInfo("~~~~"); !ok || ch != '~' || length != 4 {
		t.Errorf("fenceInfo(~~~~) = %q %d %v, want '~' 4 true", ch, length, ok)
	}
	if _, _, ok := fenceInfo("not a fence"); ok {
		t.Errorf("fenceInfo(not a fence) = ok, want not ok")
	}
}

func TestReadFrontmatterSlugAndID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.mdx")
	content := "---\nid: advanced-guides\nslug: /advanced-guides\ntitle: Advanced Guides\n---\n\n# Advanced Guides\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	slug, id, err := readFrontmatterSlugAndID(path)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "/advanced-guides" {
		t.Errorf("slug = %q, want %q", slug, "/advanced-guides")
	}
	if id != "advanced-guides" {
		t.Errorf("id = %q, want %q", id, "advanced-guides")
	}
}

func TestReadFrontmatterSlugAndID_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.mdx")
	if err := os.WriteFile(path, []byte("# Just a heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	slug, id, err := readFrontmatterSlugAndID(path)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "" || id != "" {
		t.Errorf("expected empty slug/id, got slug=%q id=%q", slug, id)
	}
}

func TestIsDocsExcluded(t *testing.T) {
	cases := map[string]bool{
		"README.md":                        true,
		"CONTRIBUTING_DOCS.md":             true,
		"DOCUMENTATION_STYLE_GUIDE.md":     true,
		"QUALITY_TOOLS_QUICK_REFERENCE.md": true,
		"_templates/guide-template.mdx":    true,
		"scripts/README.md":                true,
		"02-getting-started/index.mdx":     false,
		"guides/STREAMING.md":              false,
	}
	for in, want := range cases {
		if got := isDocsExcluded(filepath.FromSlash(in)); got != want {
			t.Errorf("isDocsExcluded(%q) = %v, want %v", in, got, want)
		}
	}
}
