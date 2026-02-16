//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ExampleExtractor extracts and validates code examples from documentation
type ExampleExtractor struct {
	docsRoot    string
	outputDir   string
	examples    []CodeExample
	verbose     bool
	testOnly    bool
	results     []TestResult
}

// CodeExample represents a code block extracted from documentation
type CodeExample struct {
	SourceFile  string
	Language    string
	Code        string
	LineNumber  int
	BlockNumber int
	IsComplete  bool // Has package main and imports
}

// TestResult represents the result of testing an example
type TestResult struct {
	Example CodeExample
	Passed  bool
	Output  string
	Error   string
}

var (
	// Matches code blocks: ```go ... ```
	codeBlockStartRegex = regexp.MustCompile("^```(\w+)\s*$")
	codeBlockEndRegex   = regexp.MustCompile("^```\s*$")
)

func main() {
	var (
		docsPath  = flag.String("docs", "./", "Path to documentation root directory")
		outputDir = flag.String("output", "./examples-extracted", "Directory to output extracted examples")
		verbose   = flag.Bool("verbose", false, "Enable verbose output")
		testOnly  = flag.Bool("test-only", false, "Only test examples, don't extract to files")
		compile   = flag.Bool("compile", true, "Attempt to compile Go examples")
	)
	flag.Parse()

	// Validate docs path
	if _, err := os.Stat(*docsPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Documentation path does not exist: %s
", *docsPath)
		os.Exit(1)
	}

	extractor := &ExampleExtractor{
		docsRoot:  *docsPath,
		outputDir: *outputDir,
		verbose:   *verbose,
		testOnly:  *testOnly,
	}

	fmt.Println("🔍 Code Example Extraction Report")
	fmt.Println("=================================")
	fmt.Printf("Documentation root: %s
", *docsPath)
	if !*testOnly {
		fmt.Printf("Output directory:   %s
", *outputDir)
	}
	fmt.Println()

	// Step 1: Extract code examples from all documentation
	if err := extractor.extractExamples(); err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting examples: %v
", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d code examples
", len(extractor.examples))
	fmt.Printf("  - %d complete Go examples
", extractor.countCompleteGoExamples())
	fmt.Printf("  - %d partial/snippet examples

", len(extractor.examples)-extractor.countCompleteGoExamples())

	// Step 2: Write examples to files (unless test-only mode)
	if !*testOnly {
		if err := extractor.writeExamples(); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing examples: %v
", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Extracted examples to: %s

", *outputDir)
	}

	// Step 3: Test/compile examples
	if *compile {
		fmt.Println("🧪 Testing Examples")
		fmt.Println("-------------------")
		extractor.testExamples()
		extractor.printTestResults()
	}

	// Exit with error if any tests failed
	failedCount := 0
	for _, result := range extractor.results {
		if !result.Passed {
			failedCount++
		}
	}

	if failedCount > 0 {
		os.Exit(1)
	}
}

// extractExamples walks the docs and extracts all code examples
func (e *ExampleExtractor) extractExamples() error {
	return filepath.Walk(e.docsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process markdown files
		ext := filepath.Ext(path)
		if ext != ".md" && ext != ".mdx" {
			return nil
		}

		// Extract examples from this file
		relPath, err := filepath.Rel(e.docsRoot, path)
		if err != nil {
			return err
		}

		return e.extractExamplesFromFile(relPath, path)
	})
}

// extractExamplesFromFile extracts code blocks from a single file
func (e *ExampleExtractor) extractExamplesFromFile(relPath, fullPath string) error {
	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	blockNumber := 0
	var currentBlock *CodeExample
	var codeLines []string

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Check for code block start
		if currentBlock == nil {
			if matches := codeBlockStartRegex.FindStringSubmatch(line); matches != nil {
				language := matches[1]
				blockNumber++
				currentBlock = &CodeExample{
					SourceFile:  relPath,
					Language:    language,
					LineNumber:  lineNumber,
					BlockNumber: blockNumber,
				}
				codeLines = []string{}

				if e.verbose {
					fmt.Printf("  Found %s code block in %s (line %d)
", language, relPath, lineNumber)
				}
			}
			continue
		}

		// Check for code block end
		if codeBlockEndRegex.MatchString(line) {
			currentBlock.Code = strings.Join(codeLines, "
")
			currentBlock.IsComplete = e.isCompleteGoExample(currentBlock.Code)
			e.examples = append(e.examples, *currentBlock)
			currentBlock = nil
			codeLines = []string{}
			continue
		}

		// Accumulate code lines
		codeLines = append(codeLines, line)
	}

	return scanner.Err()
}

// isCompleteGoExample checks if a Go code block is a complete, runnable example
func (e *ExampleExtractor) isCompleteGoExample(code string) bool {
	hasPackage := strings.Contains(code, "package main") || strings.Contains(code, "package ")
	hasFunc := strings.Contains(code, "func ")

	// Complete example should have package declaration and at least a function
	return hasPackage && hasFunc
}

// countCompleteGoExamples counts how many complete Go examples were found
func (e *ExampleExtractor) countCompleteGoExamples() int {
	count := 0
	for _, ex := range e.examples {
		if ex.Language == "go" && ex.IsComplete {
			count++
		}
	}
	return count
}

// writeExamples writes extracted examples to files
func (e *ExampleExtractor) writeExamples() error {
	// Create output directory
	if err := os.MkdirAll(e.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write each example to a file
	for i, example := range e.examples {
		if example.Language != "go" {
			continue // Only extract Go examples for now
		}

		// Generate filename
		sourceBase := strings.TrimSuffix(filepath.Base(example.SourceFile), filepath.Ext(example.SourceFile))
		filename := fmt.Sprintf("%s_example_%d.go", sourceBase, example.BlockNumber)
		outputPath := filepath.Join(e.outputDir, filename)

		// Write code to file
		if err := os.WriteFile(outputPath, []byte(example.Code), 0644); err != nil {
			return fmt.Errorf("failed to write example %d: %w", i, err)
		}

		if e.verbose {
			fmt.Printf("  Wrote: %s
", filename)
		}
	}

	return nil
}

// testExamples attempts to compile/test each example
func (e *ExampleExtractor) testExamples() {
	for _, example := range e.examples {
		// Only test Go examples
		if example.Language != "go" {
			continue
		}

		// Only test complete examples
		if !example.IsComplete {
			if e.verbose {
				fmt.Printf("⊘ Skipping incomplete example from %s (line %d)
",
					example.SourceFile, example.LineNumber)
			}
			continue
		}

		result := e.testExample(example)
		e.results = append(e.results, result)
	}
}

// testExample tests a single code example
func (e *ExampleExtractor) testExample(example CodeExample) TestResult {
	result := TestResult{
		Example: example,
		Passed:  false,
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "go-ai-example-*.go")
	if err != nil {
		result.Error = fmt.Sprintf("Failed to create temp file: %v", err)
		return result
	}
	defer os.Remove(tmpFile.Name())

	// Write code to temp file
	if _, err := tmpFile.WriteString(example.Code); err != nil {
		result.Error = fmt.Sprintf("Failed to write code: %v", err)
		return result
	}
	tmpFile.Close()

	// Try to compile
	cmd := exec.Command("go", "build", "-o", "/dev/null", tmpFile.Name())
	output, err := cmd.CombinedOutput()

	if err != nil {
		result.Error = fmt.Sprintf("Compilation failed: %v", err)
		result.Output = string(output)
		fmt.Printf("❌ FAIL: %s (line %d)
", example.SourceFile, example.LineNumber)
		if e.verbose {
			fmt.Printf("   Error: %s
", result.Error)
			if len(output) > 0 {
				fmt.Printf("   Output:
%s
", indentLines(string(output), "      "))
			}
		}
	} else {
		result.Passed = true
		fmt.Printf("✅ PASS: %s (line %d)
", example.SourceFile, example.LineNumber)
	}

	return result
}

// printTestResults prints a summary of test results
func (e *ExampleExtractor) printTestResults() {
	fmt.Println("
📊 Test Summary")
	fmt.Println("---------------")

	passedCount := 0
	failedCount := 0
	for _, result := range e.results {
		if result.Passed {
			passedCount++
		} else {
			failedCount++
		}
	}

	totalTested := len(e.results)
	totalExamples := len(e.examples)

	fmt.Printf("Total examples found:    %d
", totalExamples)
	fmt.Printf("Examples tested:         %d
", totalTested)
	fmt.Printf("Passed:                  %d
", passedCount)
	fmt.Printf("Failed:                  %d

", failedCount)

	if failedCount > 0 {
		fmt.Println("❌ Failed Examples:")
		fmt.Println("-------------------")
		for _, result := range e.results {
			if !result.Passed {
				fmt.Printf("
📄 %s (line %d, block %d)
",
					result.Example.SourceFile,
					result.Example.LineNumber,
					result.Example.BlockNumber)
				fmt.Printf("   Error: %s
", result.Error)
				if len(result.Output) > 0 && e.verbose {
					fmt.Printf("   Compiler output:
%s
", indentLines(result.Output, "      "))
				}
			}
		}

		fmt.Println("
💡 Common Issues:")
		fmt.Println("  • Missing imports (add required packages)")
		fmt.Println("  • Placeholder values (replace 'your-api-key' with actual values)")
		fmt.Println("  • Pseudo-code (examples meant for illustration only)")
		fmt.Println("  • Incomplete examples (snippets showing specific features)")
	} else if totalTested > 0 {
		fmt.Println("✅ All tested examples compiled successfully!")
	}

	// Provide guidance on untested examples
	untestedCount := totalExamples - totalTested
	if untestedCount > 0 {
		fmt.Printf("
ℹ️  %d examples were not tested (incomplete snippets or non-Go code)
", untestedCount)
	}
}

// indentLines adds indentation to each line of a string
func indentLines(s, indent string) string {
	lines := strings.Split(s, "
")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "
")
}
