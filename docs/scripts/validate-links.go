package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LinkValidator validates internal links in documentation files
type LinkValidator struct {
	docsRoot    string
	files       map[string]bool
	docRoutes   map[string]string // Docusaurus route suffix (no leading/trailing slash) -> file path relative to docsRoot
	links       []Link
	brokenLinks []BrokenLink
	verbose     bool
}

// Link represents a markdown link found in documentation
type Link struct {
	SourceFile string
	TargetPath string
	LineNumber int
	LinkText   string
}

// BrokenLink represents a link that points to a non-existent file
type BrokenLink struct {
	Link
	ResolvedPath string
	Error        string
}

// Regular expressions for finding markdown links
var (
	// Matches [text](path.mdx) and [text](path.mdx#anchor)
	markdownLinkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Matches <reference to="path.mdx" />
	referenceRegex = regexp.MustCompile(`<reference\s+to="([^"]+)"\s*/>`)

	// Matches inline code spans, e.g. `ai.ObjectOutput[T](...)`. Content inside
	// these (and inside fenced code blocks, handled separately) is illustrative
	// Go/shell/markdown syntax, not real links, and must not be scanned.
	inlineCodeSpanRegex = regexp.MustCompile("`[^`]*`")

	// Matches a fenced code block delimiter line (``` or ~~~, optionally indented
	// up to 3 spaces per CommonMark, optionally followed by an info string).
	fenceDelimiterRegex = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

	// Matches YAML frontmatter delimiters.
	frontmatterDelimiterRegex = regexp.MustCompile(`^---\s*$`)

	// Matches `slug: value` / `id: value` frontmatter fields (optionally quoted).
	frontmatterSlugRegex = regexp.MustCompile(`^slug:\s*["']?([^"'\s]+)["']?\s*$`)
	frontmatterIDRegex   = regexp.MustCompile(`^id:\s*["']?([^"'\s]+)["']?\s*$`)

	// Strips a Docusaurus numeric prefix from a single path segment, e.g.
	// "05-providers" -> "providers", "39-gateway.mdx" -> "gateway.mdx".
	// Mirrors Docusaurus's default numberPrefixParser, which strips a leading
	// run of digits followed by one of '.', '-', '_' or whitespace.
	numberPrefixRegex = regexp.MustCompile(`^[0-9]+[-_. ]+(.+)$`)
)

// docsExcludes mirrors the `exclude` list in website/docusaurus.config.ts's docs
// preset. Files matching these are on disk (and their own links are still
// checked normally) but are not part of the published site, so they never
// occupy a Docusaurus doc route and other pages' /docs/... links can never
// resolve to them.
var docsExcludes = []string{
	"README.md",
	"CONTRIBUTING_DOCS.md",
	"DOCUMENTATION_STYLE_GUIDE.md",
	"QUALITY_TOOLS_QUICK_REFERENCE.md",
}

// docsExcludeDirs mirrors the directory globs in the same `exclude` list.
var docsExcludeDirs = []string{
	"_templates",
	"scripts",
	"implementation",
}

// linkAllowlist lists specific (source file, raw target) pairs that are known
// to be unresolvable by this validator but are not documentation bugs. Keep
// this list narrow and comment every entry.
var linkAllowlist = map[string]bool{
	// (none currently — every link found in docs/ resolves to a real target
	// or a real filesystem path)
}

func main() {
	var (
		docsPath = flag.String("docs", "./", "Path to documentation root directory")
		verbose  = flag.Bool("verbose", false, "Enable verbose output")
	)
	flag.Parse()

	// Validate docs path exists
	if _, err := os.Stat(*docsPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Documentation path does not exist: %s\n", *docsPath)
		os.Exit(1)
	}

	validator := &LinkValidator{
		docsRoot:  *docsPath,
		files:     make(map[string]bool),
		docRoutes: make(map[string]string),
		verbose:   *verbose,
	}

	fmt.Println("🔍 Link Validation Report")
	fmt.Println("=======================")
	fmt.Printf("Documentation root: %s\n\n", *docsPath)

	// Step 1: Discover all documentation files
	if err := validator.discoverFiles(); err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering files: %v\n", err)
		os.Exit(1)
	}

	if validator.verbose {
		fmt.Printf("Found %d documentation files\n\n", len(validator.files))
	}

	// Step 1b: Build the Docusaurus route map (routeBasePath resolution) so
	// site-absolute /docs/... links can be validated the way Docusaurus
	// resolves them: numeric-prefix stripping, index files, and frontmatter
	// slug/id overrides.
	if err := validator.buildDocRoutes(); err != nil {
		fmt.Fprintf(os.Stderr, "Error building doc routes: %v\n", err)
		os.Exit(1)
	}

	if validator.verbose {
		fmt.Printf("Resolved %d Docusaurus doc routes\n\n", len(validator.docRoutes))
	}

	// Step 2: Extract all links from documentation
	if err := validator.extractLinks(); err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting links: %v\n", err)
		os.Exit(1)
	}

	if validator.verbose {
		fmt.Printf("Found %d links to validate\n\n", len(validator.links))
	}

	// Step 3: Validate each link
	validator.validateLinks()

	// Step 4: Generate report
	validator.printReport()

	// Exit with error code if broken links found
	if len(validator.brokenLinks) > 0 {
		os.Exit(1)
	}
}

// discoverFiles walks the docs directory and catalogs all .mdx and .md files
func (v *LinkValidator) discoverFiles() error {
	return filepath.Walk(v.docsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() {
			// Skip node_modules, .git, and other common directories
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process markdown files
		ext := filepath.Ext(path)
		if ext == ".md" || ext == ".mdx" {
			// Store relative path from docs root
			relPath, err := filepath.Rel(v.docsRoot, path)
			if err != nil {
				return err
			}
			v.files[relPath] = true

			if v.verbose {
				fmt.Printf("  Found: %s\n", relPath)
			}
		}

		return nil
	})
}

// buildDocRoutes computes, for every discovered doc file that Docusaurus would
// actually publish, the route it is served at under routeBasePath ("docs").
// This lets validateLinks resolve site-absolute links like
// "/docs/ai-sdk-core/video-generation" the way Docusaurus does.
func (v *LinkValidator) buildDocRoutes() error {
	for relPath := range v.files {
		if isDocsExcluded(relPath) {
			continue
		}

		slug, id, err := readFrontmatterSlugAndID(filepath.Join(v.docsRoot, relPath))
		if err != nil {
			return fmt.Errorf("error reading frontmatter for %s: %w", relPath, err)
		}

		route := routeForFile(relPath, slug, id)
		v.docRoutes[route] = relPath
	}
	return nil
}

// isDocsExcluded reports whether relPath (slash-normalized, relative to
// docsRoot) matches the `exclude` list Docusaurus uses for the docs plugin
// (see website/docusaurus.config.ts). Excluded files are not part of the
// published site, so they never claim a doc route.
func isDocsExcluded(relPath string) bool {
	slashPath := filepath.ToSlash(relPath)

	for _, name := range docsExcludes {
		if slashPath == name {
			return true
		}
	}

	for _, dir := range docsExcludeDirs {
		if slashPath == dir || strings.HasPrefix(slashPath, dir+"/") {
			return true
		}
	}

	return false
}

// stripNumberedPrefix strips a Docusaurus-style numeric prefix (digits
// followed by '.', '-', '_' or whitespace) from a single path segment.
func stripNumberedPrefix(segment string) string {
	if m := numberPrefixRegex.FindStringSubmatch(segment); m != nil {
		return m[1]
	}
	return segment
}

// routeForFile computes the Docusaurus doc route (relative to routeBasePath,
// no leading/trailing slash) for a doc file at relPath, honoring an explicit
// frontmatter `slug` (or, failing that, `id`) override exactly like
// Docusaurus: an absolute slug ("/foo") replaces the whole route, a relative
// slug replaces only the final path segment.
func routeForFile(relPath, slug, id string) string {
	defaultRoute := computeDefaultRoute(relPath)

	override := slug
	if override == "" {
		override = id
	}
	if override == "" {
		return defaultRoute
	}

	if strings.HasPrefix(override, "/") {
		return strings.Trim(override, "/")
	}

	// Relative override: keep the directory portion, replace the last segment.
	dir := defaultRoute
	if idx := strings.LastIndex(defaultRoute, "/"); idx != -1 {
		dir = defaultRoute[:idx]
	} else {
		dir = ""
	}
	if dir == "" {
		return strings.Trim(override, "/")
	}
	return dir + "/" + strings.Trim(override, "/")
}

// computeDefaultRoute applies Docusaurus's default routing rules (no
// frontmatter overrides): strip the numeric prefix from every path segment
// (directories and filename), drop the extension, and drop a segment whose
// stripped name is "index" (an index file's route is its directory).
func computeDefaultRoute(relPath string) string {
	slashPath := filepath.ToSlash(relPath)
	segments := strings.Split(slashPath, "/")

	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		if i == len(segments)-1 {
			seg = strings.TrimSuffix(seg, filepath.Ext(seg))
		}
		seg = stripNumberedPrefix(seg)
		if i == len(segments)-1 && strings.EqualFold(seg, "index") {
			continue // index files are served at their directory's route
		}
		out = append(out, seg)
	}
	return strings.Join(out, "/")
}

// readFrontmatterSlugAndID reads the `slug` and `id` fields out of a doc
// file's YAML frontmatter, if present. Returns empty strings if there is no
// frontmatter or no such fields.
func readFrontmatterSlugAndID(fullPath string) (slug string, id string, err error) {
	file, err := os.Open(fullPath)
	if err != nil {
		return "", "", err
	}
	defer file.Close() //nolint:errcheck

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return "", "", scanner.Err()
	}
	if !frontmatterDelimiterRegex.MatchString(scanner.Text()) {
		return "", "", nil // no frontmatter
	}

	for scanner.Scan() {
		line := scanner.Text()
		if frontmatterDelimiterRegex.MatchString(line) {
			break // closing "---"
		}
		if m := frontmatterSlugRegex.FindStringSubmatch(line); m != nil {
			slug = m[1]
		} else if m := frontmatterIDRegex.FindStringSubmatch(line); m != nil {
			id = m[1]
		}
	}
	return slug, id, scanner.Err()
}

// extractLinks scans all files and extracts markdown links
func (v *LinkValidator) extractLinks() error {
	for filePath := range v.files {
		fullPath := filepath.Join(v.docsRoot, filePath)
		if err := v.extractLinksFromFile(filePath, fullPath); err != nil {
			return fmt.Errorf("error processing %s: %w", filePath, err)
		}
	}
	return nil
}

// stripInlineCode blanks out inline code spans (`...`) in a single line so
// that link-like syntax inside them (e.g. Go generics such as
// `ai.ObjectOutput[T](...)`) is never mistaken for a Markdown link. The
// replacement preserves line length/column positions.
func stripInlineCode(line string) string {
	return inlineCodeSpanRegex.ReplaceAllStringFunc(line, func(span string) string {
		return strings.Repeat(" ", len(span))
	})
}

// fenceInfo reports whether line is a fenced-code-block delimiter, and if so,
// the delimiter character and how many times it repeats.
func fenceInfo(line string) (ch byte, length int, ok bool) {
	m := fenceDelimiterRegex.FindString(line)
	if m == "" {
		return 0, 0, false
	}
	marker := strings.TrimLeft(m, " ")
	return marker[0], len(marker), true
}

// extractLinksFromFile extracts all links from a single file, skipping the
// contents of fenced code blocks and inline code spans (neither of which
// contain real Markdown links, only illustrative syntax).
func (v *LinkValidator) extractLinksFromFile(relPath, fullPath string) error {
	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	inFence := false
	var fenceChar byte
	var fenceLen int

	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()

		if ch, length, ok := fenceInfo(rawLine); ok {
			if inFence {
				if ch == fenceChar && length >= fenceLen {
					inFence = false
				}
			} else {
				inFence = true
				fenceChar = ch
				fenceLen = length
			}
			continue // fence delimiter lines are never scanned for links
		}

		if inFence {
			continue
		}

		line := stripInlineCode(rawLine)

		// Find markdown links: [text](path)
		matches := markdownLinkRegex.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				linkText := match[1]
				targetPath := match[2]

				// Skip external links (http://, https://, mailto:, etc.)
				if isExternalLink(targetPath) {
					continue
				}

				// Skip anchors-only links (#section)
				if strings.HasPrefix(targetPath, "#") {
					continue
				}

				v.links = append(v.links, Link{
					SourceFile: relPath,
					TargetPath: targetPath,
					LineNumber: lineNumber,
					LinkText:   linkText,
				})
			}
		}

		// Find reference components: <reference to="path" />
		refMatches := referenceRegex.FindAllStringSubmatch(line, -1)
		for _, match := range refMatches {
			if len(match) >= 2 {
				targetPath := match[1]
				if !isExternalLink(targetPath) {
					v.links = append(v.links, Link{
						SourceFile: relPath,
						TargetPath: targetPath,
						LineNumber: lineNumber,
						LinkText:   "(reference component)",
					})
				}
			}
		}
	}

	return scanner.Err()
}

// validateLinks checks if each link target exists
func (v *LinkValidator) validateLinks() {
	for _, link := range v.links {
		if linkAllowlist[link.SourceFile+"|"+link.TargetPath] {
			continue
		}

		// Remove anchor if present (#section)
		targetPath := link.TargetPath
		if idx := strings.Index(targetPath, "#"); idx != -1 {
			targetPath = targetPath[:idx]
		}
		if targetPath == "" {
			continue // anchor-only after stripping (shouldn't happen; already filtered)
		}

		// Site-absolute links (e.g. "/docs/ai-sdk-core/video-generation") are
		// resolved by Docusaurus via routeBasePath, not via the filesystem.
		if strings.HasPrefix(targetPath, "/docs/") || targetPath == "/docs" {
			suffix := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(targetPath, "/docs"), "/"), "/")
			if _, ok := v.docRoutes[suffix]; ok {
				continue
			}
			v.brokenLinks = append(v.brokenLinks, BrokenLink{
				Link:         link,
				ResolvedPath: "/docs/" + suffix,
				Error:        "No matching Docusaurus doc route for site-absolute link",
			})
			continue
		}

		// Resolve relative path from source file
		sourceDir := filepath.Dir(link.SourceFile)
		resolvedPath := filepath.Join(sourceDir, targetPath)

		// Normalize path (remove .. and .)
		resolvedPath = filepath.Clean(resolvedPath)

		if v.files[resolvedPath] {
			continue
		}

		// Try with .mdx/.md extension if not already present
		if !strings.HasSuffix(resolvedPath, ".mdx") && !strings.HasSuffix(resolvedPath, ".md") {
			if v.files[resolvedPath+".mdx"] || v.files[resolvedPath+".md"] {
				continue
			}
		}

		// Fall back to an actual filesystem check. This covers targets the
		// markdown-file index above can't represent: non-.md/.mdx files
		// (example .go files, images, LICENSE, ...), directories (GitHub
		// renders a link to a directory as its listing), and paths that walk
		// above docsRoot (e.g. "../../examples/..." reaching the repo-root
		// examples/ directory).
		if _, err := os.Stat(filepath.Join(v.docsRoot, resolvedPath)); err == nil {
			continue
		}

		// Link is broken
		v.brokenLinks = append(v.brokenLinks, BrokenLink{
			Link:         link,
			ResolvedPath: resolvedPath,
			Error:        "Target file does not exist",
		})
	}
}

// printReport generates and prints the validation report
func (v *LinkValidator) printReport() {
	fmt.Printf("📊 Summary\n")
	fmt.Println("----------")
	fmt.Printf("Total files scanned:   %d\n", len(v.files))
	fmt.Printf("Total links found:     %d\n", len(v.links))
	fmt.Printf("Broken links:          %d\n\n", len(v.brokenLinks))

	if len(v.brokenLinks) == 0 {
		fmt.Println("✅ All links are valid!")
		return
	}

	fmt.Println("❌ Broken Links Found:")
	fmt.Println("---------------------")

	// Group broken links by source file
	linksByFile := make(map[string][]BrokenLink)
	for _, broken := range v.brokenLinks {
		linksByFile[broken.SourceFile] = append(linksByFile[broken.SourceFile], broken)
	}

	// Print grouped by file
	for sourceFile, links := range linksByFile {
		fmt.Printf("\n📄 %s\n", sourceFile)
		for _, link := range links {
			fmt.Printf("   Line %d: [%s](%s)\n", link.LineNumber, link.LinkText, link.TargetPath)
			fmt.Printf("           → Resolved to: %s\n", link.ResolvedPath)
			fmt.Printf("           → Error: %s\n", link.Error)
		}
	}

	fmt.Println("\n💡 Suggestions:")
	fmt.Println("  • Check if the target file exists")
	fmt.Println("  • Verify the relative path is correct")
	fmt.Println("  • Ensure the file extension (.mdx or .md) is included")
	fmt.Println("  • Check for typos in the file name")
}

// isExternalLink checks if a link points to an external resource
func isExternalLink(path string) bool {
	prefixes := []string{"http://", "https://", "mailto:", "tel:", "ftp://", "//"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
