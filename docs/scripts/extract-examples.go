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

// ExampleExtractor extracts and validates code examples from documentation.
type ExampleExtractor struct {
	docsRoot  string
	outputDir string
	examples  []CodeExample
	verbose   bool
	testOnly  bool
	results   []TestResult
}

// CodeExample represents a code block extracted from documentation.
type CodeExample struct {
	SourceFile  string
	Language    string
	Code        string
	LineNumber  int
	BlockNumber int
	IsComplete  bool
}

// TestResult represents the result of testing an example.
type TestResult struct {
	Example CodeExample
	Passed  bool
	Output  string
	Error   string
}

var (
	codeBlockStartRegex = regexp.MustCompile("^```(\\w+)\\s*$")
	codeBlockEndRegex   = regexp.MustCompile("^```\\s*$")
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

	if _, err := os.Stat(*docsPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Documentation path does not exist: %s\n", *docsPath)
		os.Exit(1)
	}

	extractor := &ExampleExtractor{
		docsRoot:  *docsPath,
		outputDir: *outputDir,
		verbose:   *verbose,
		testOnly:  *testOnly,
	}

	fmt.Println("Code Example Extraction Report")
	fmt.Println("==============================")
	fmt.Printf("Documentation root: %s\n", *docsPath)
	if !*testOnly {
		fmt.Printf("Output directory:   %s\n", *outputDir)
	}
	fmt.Println()

	if err := extractor.extractExamples(); err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting examples: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d code examples\n", len(extractor.examples))
	fmt.Printf("  - %d complete Go examples\n", extractor.countCompleteGoExamples())
	fmt.Printf("  - %d partial/snippet examples\n\n", len(extractor.examples)-extractor.countCompleteGoExamples())

	if !*testOnly {
		if err := extractor.writeExamples(); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing examples: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Extracted examples to: %s\n\n", *outputDir)
	}

	if *compile {
		fmt.Println("Testing Examples")
		fmt.Println("----------------")
		extractor.testExamples()
		extractor.printTestResults()
	}

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

func (e *ExampleExtractor) extractExamples() error {
	return filepath.Walk(e.docsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == ".next" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		relPath, err := filepath.Rel(e.docsRoot, path)
		if err != nil {
			return err
		}
		return e.extractExamplesFromFile(relPath, path)
	})
}

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
					fmt.Printf("  Found %s code block in %s (line %d)\n", language, relPath, lineNumber)
				}
			}
			continue
		}
		if codeBlockEndRegex.MatchString(line) {
			currentBlock.Code = strings.Join(codeLines, "\n")
			currentBlock.IsComplete = e.isCompleteGoExample(currentBlock.Code)
			e.examples = append(e.examples, *currentBlock)
			currentBlock = nil
			codeLines = []string{}
			continue
		}
		codeLines = append(codeLines, line)
	}
	return scanner.Err()
}

func (e *ExampleExtractor) isCompleteGoExample(code string) bool {
	hasPackage := strings.Contains(code, "package main") || strings.Contains(code, "package ")
	hasFunc := strings.Contains(code, "func ")
	return hasPackage && hasFunc
}

func (e *ExampleExtractor) countCompleteGoExamples() int {
	count := 0
	for _, ex := range e.examples {
		if ex.Language == "go" && ex.IsComplete {
			count++
		}
	}
	return count
}

func (e *ExampleExtractor) writeExamples() error {
	if err := os.MkdirAll(e.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	for i, example := range e.examples {
		if example.Language != "go" {
			continue
		}
		sourceBase := strings.TrimSuffix(filepath.Base(example.SourceFile), filepath.Ext(example.SourceFile))
		filename := fmt.Sprintf("%s_example_%d.go", sourceBase, example.BlockNumber)
		outputPath := filepath.Join(e.outputDir, filename)
		if err := os.WriteFile(outputPath, []byte(example.Code), 0644); err != nil {
			return fmt.Errorf("failed to write example %d: %w", i, err)
		}
		if e.verbose {
			fmt.Printf("  Wrote: %s\n", filename)
		}
	}
	return nil
}

func (e *ExampleExtractor) testExamples() {
	for _, example := range e.examples {
		if example.Language != "go" || !example.IsComplete {
			if e.verbose && example.Language == "go" {
				fmt.Printf("Skipping incomplete example from %s (line %d)\n", example.SourceFile, example.LineNumber)
			}
			continue
		}
		e.results = append(e.results, e.testExample(example))
	}
}

func (e *ExampleExtractor) testExample(example CodeExample) TestResult {
	result := TestResult{Example: example}
	tmpFile, err := os.CreateTemp("", "go-ai-example-*.go")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create temp file: %v", err)
		return result
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(example.Code); err != nil {
		result.Error = fmt.Sprintf("failed to write code: %v", err)
		return result
	}
	if err := tmpFile.Close(); err != nil {
		result.Error = fmt.Sprintf("failed to close temp file: %v", err)
		return result
	}

	cmd := exec.Command("go", "build", "-o", "/dev/null", tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("compilation failed: %v", err)
		result.Output = string(output)
		fmt.Printf("FAIL: %s (line %d)\n", example.SourceFile, example.LineNumber)
		if e.verbose {
			fmt.Printf("   Error: %s\n", result.Error)
			if len(output) > 0 {
				fmt.Printf("   Output:\n%s\n", indentLines(string(output), "      "))
			}
		}
		return result
	}

	result.Passed = true
	fmt.Printf("PASS: %s (line %d)\n", example.SourceFile, example.LineNumber)
	return result
}

func (e *ExampleExtractor) printTestResults() {
	fmt.Println("\nTest Summary")
	fmt.Println("------------")

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
	fmt.Printf("Total examples found:    %d\n", totalExamples)
	fmt.Printf("Examples tested:         %d\n", totalTested)
	fmt.Printf("Passed:                  %d\n", passedCount)
	fmt.Printf("Failed:                  %d\n\n", failedCount)

	if failedCount > 0 {
		fmt.Println("Failed Examples:")
		fmt.Println("----------------")
		for _, result := range e.results {
			if !result.Passed {
				fmt.Printf("\n%s (line %d, block %d)\n", result.Example.SourceFile, result.Example.LineNumber, result.Example.BlockNumber)
				fmt.Printf("   Error: %s\n", result.Error)
				if len(result.Output) > 0 && e.verbose {
					fmt.Printf("   Compiler output:\n%s\n", indentLines(result.Output, "      "))
				}
			}
		}
		fmt.Println("\nCommon Issues:")
		fmt.Println("  - Missing imports")
		fmt.Println("  - Example values that need local credentials")
		fmt.Println("  - Pseudo-code examples meant for illustration only")
		fmt.Println("  - Incomplete snippets showing specific features")
	} else if totalTested > 0 {
		fmt.Println("All tested examples compiled successfully.")
	}

	untestedCount := totalExamples - totalTested
	if untestedCount > 0 {
		fmt.Printf("\n%d examples were not tested because they are incomplete snippets or non-Go code.\n", untestedCount)
	}
}

func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}
