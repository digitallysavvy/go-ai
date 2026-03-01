// Package main demonstrates Anthropic code execution tool (20260120) with text editor operations.
//
// This example shows how to handle text editor operations requested by the model:
// - view: Read file contents
// - create: Create or overwrite a file
// - str_replace: Replace a string within a file
//
// The model requests file operations via the code execution tool, and the client
// handles each operation locally, returning results back to the model.
//
// Run with:
//
//	ANTHROPIC_API_KEY=... go run main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	anthropicTools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: ANTHROPIC_API_KEY environment variable is required")
		os.Exit(1)
	}

	// Create a temp file for the model to work with
	tmpFile, err := os.CreateTemp("", "code-exec-example-*.txt")
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.WriteString("Hello, World!\nThis is a test file.\n")
	tmpFile.Close()
	defer os.Remove(tmpPath)

	fmt.Printf("Working with temp file: %s\n\n", tmpPath)

	prov := anthropic.New(anthropic.Config{APIKey: apiKey})
	model, err := prov.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Create the code execution tool with a client-side text editor handler
	codeExecTool := anthropicTools.CodeExecution20260120()
	codeExecTool.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return handleCodeExecution(ctx, input)
	}

	prompt := fmt.Sprintf(
		"Use the code execution tool to:\n"+
			"1. View the contents of %s\n"+
			"2. Replace 'Hello, World!' with 'Hello, Claude!' in that file\n"+
			"3. View the file again to confirm the change\n"+
			"Describe what you did and what you found.",
		tmpPath,
	)

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: prompt,
		Tools:  []types.Tool{codeExecTool},
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Println("=== Model Response ===")
	fmt.Println(result.Text)

	if len(result.ToolResults) > 0 {
		fmt.Printf("\n=== Tool Executions (%d) ===\n", len(result.ToolResults))
		for _, tr := range result.ToolResults {
			fmt.Printf("Tool: %s\n", tr.ToolName)
			if tr.Error != nil {
				fmt.Printf("  Error: %v\n", tr.Error)
			} else {
				fmt.Printf("  Result: %v\n", tr.Result)
			}
		}
	}

	// Show final file contents
	finalContents, err := os.ReadFile(tmpPath)
	if err == nil {
		fmt.Printf("\n=== Final File Contents ===\n%s\n", finalContents)
	}
}

// handleCodeExecution routes a code execution tool call to the appropriate handler.
func handleCodeExecution(ctx context.Context, rawInput map[string]interface{}) (interface{}, error) {
	inputJSON, err := json.Marshal(rawInput)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	input, err := anthropicTools.UnmarshalCodeExecutionInput(inputJSON)
	if err != nil {
		return nil, fmt.Errorf("parse code execution input: %w", err)
	}

	switch v := input.(type) {
	case *anthropicTools.TextEditorInput:
		return handleTextEditor(v)

	case *anthropicTools.BashCodeExecutionInput:
		return nil, fmt.Errorf("bash execution not supported in this example; use text editor commands instead")

	case *anthropicTools.ProgrammaticToolCallInput:
		return nil, fmt.Errorf("programmatic execution not supported in this example")

	default:
		return nil, fmt.Errorf("unexpected input type: %T", input)
	}
}

// handleTextEditor executes a text editor operation and returns the appropriate result type.
func handleTextEditor(input *anthropicTools.TextEditorInput) (anthropicTools.CodeExecutionResult, error) {
	fmt.Printf("[TEXT EDITOR] command=%s path=%s\n", input.Command, input.Path)

	switch input.Command {
	case "view":
		return viewFile(input.Path)
	case "create":
		return createFile(input.Path, input.FileText)
	case "str_replace":
		return strReplaceFile(input.Path, input.OldStr, input.NewStr)
	default:
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "invalid_tool_input",
		}, nil
	}
}

// viewFile reads a file and returns a TextEditorViewResult.
func viewFile(path string) (anthropicTools.CodeExecutionResult, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "file_not_found",
		}, nil
	}
	if err != nil {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "unavailable",
		}, nil
	}

	content := string(data)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	numLines := len(lines)
	startLine := 1
	totalLines := numLines

	return &anthropicTools.TextEditorViewResult{
		Type:       anthropicTools.CodeExecutionResultTypeViewResult,
		Content:    content,
		FileType:   "text",
		NumLines:   &numLines,
		StartLine:  &startLine,
		TotalLines: &totalLines,
	}, nil
}

// createFile creates or overwrites a file with optional content.
func createFile(path string, fileText *string) (anthropicTools.CodeExecutionResult, error) {
	content := ""
	if fileText != nil {
		content = *fileText
	}

	// Check if file exists for is_file_update
	_, statErr := os.Stat(path)
	isUpdate := statErr == nil

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "unavailable",
		}, nil
	}

	return &anthropicTools.TextEditorCreateResult{
		Type:         anthropicTools.CodeExecutionResultTypeCreateResult,
		IsFileUpdate: isUpdate,
	}, nil
}

// strReplaceFile replaces a string in a file and returns a TextEditorStrReplaceResult.
func strReplaceFile(path string, oldStr, newStr *string) (anthropicTools.CodeExecutionResult, error) {
	if oldStr == nil || newStr == nil {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "invalid_tool_input",
		}, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "file_not_found",
		}, nil
	}
	if err != nil {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "unavailable",
		}, nil
	}

	original := string(data)
	replaced := strings.Replace(original, *oldStr, *newStr, 1)

	if replaced == original {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "invalid_tool_input",
		}, nil
	}

	if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
		return &anthropicTools.TextEditorExecutionError{
			Type:      anthropicTools.CodeExecutionResultTypeTextEditorError,
			ErrorCode: "unavailable",
		}, nil
	}

	// Calculate line info for the result
	newLines := strings.Split(strings.TrimRight(*newStr, "\n"), "\n")
	oldLines := strings.Split(strings.TrimRight(*oldStr, "\n"), "\n")
	newLinesCount := len(newLines)
	oldLinesCount := len(oldLines)

	// Find the starting line of the replacement
	beforeReplacement := original[:strings.Index(original, *oldStr)]
	oldStart := strings.Count(beforeReplacement, "\n") + 1
	newStart := oldStart

	return &anthropicTools.TextEditorStrReplaceResult{
		Type:     anthropicTools.CodeExecutionResultTypeStrReplaceResult,
		Lines:    newLines,
		NewLines: &newLinesCount,
		NewStart: &newStart,
		OldLines: &oldLinesCount,
		OldStart: &oldStart,
	}, nil
}
