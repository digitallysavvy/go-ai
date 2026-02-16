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
	docsRoot     string
	files        map[string]bool
	links        []Link
	brokenLinks  []BrokenLink
	verbose      bool
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

	// Matches import statements that might reference other docs
	importRegex = regexp.MustCompile(`import\s+.*from\s+['"]([^'"]+)['"]`)
)

func main() {
	var (
		docsPath = flag.String("docs", "./", "Path to documentation root directory")
		verbose  = flag.Bool("verbose", false, "Enable verbose output")
		fix      = flag.Bool("fix", false, "Attempt to fix broken links (not implemented)")
	)
	flag.Parse()

	// Validate docs path exists
	if _, err := os.Stat(*docsPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Documentation path does not exist: %s\n", *docsPath)
		os.Exit(1)
	}

	validator := &LinkValidator{
		docsRoot: *docsPath,
		files:    make(map[string]bool),
		verbose:  *verbose,
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

	if *fix {
		fmt.Println("\n⚠️  Auto-fix feature is not yet implemented")
	}

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

// extractLinksFromFile extracts all links from a single file
func (v *LinkValidator) extractLinksFromFile(relPath, fullPath string) error {
	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

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
		// Remove anchor if present (#section)
		targetPath := link.TargetPath
		if idx := strings.Index(targetPath, "#"); idx != -1 {
			targetPath = targetPath[:idx]
		}

		// Resolve relative path from source file
		sourceDir := filepath.Dir(link.SourceFile)
		resolvedPath := filepath.Join(sourceDir, targetPath)

		// Normalize path (remove .. and .)
		resolvedPath = filepath.Clean(resolvedPath)

		// Check if target file exists
		if !v.files[resolvedPath] {
			// Try with .mdx extension if not present
			if !strings.HasSuffix(resolvedPath, ".mdx") && !strings.HasSuffix(resolvedPath, ".md") {
				if v.files[resolvedPath+".mdx"] {
					continue
				}
				if v.files[resolvedPath+".md"] {
					continue
				}
			}

			// Link is broken
			v.brokenLinks = append(v.brokenLinks, BrokenLink{
				Link:         link,
				ResolvedPath: resolvedPath,
				Error:        "Target file does not exist",
			})
		}
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
