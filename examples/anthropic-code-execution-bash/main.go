// Package main demonstrates Anthropic code execution tool (20260120) with bash execution.
//
// This example shows how to use the code execution tool to let Claude run shell commands.
// The model requests bash execution via the code execution tool, and the client handles
// the execution locally, returning results back to the model.
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
	"os/exec"
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

	prov := anthropic.New(anthropic.Config{APIKey: apiKey})
	model, err := prov.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Create the code execution tool with a client-side bash executor
	codeExecTool := anthropicTools.CodeExecution20260120()
	codeExecTool.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return handleCodeExecution(ctx, input)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Prompt: "Use the code execution tool to:\n" +
			"1. Check the current date with: date\n" +
			"2. List files in the current directory with: ls -la\n" +
			"Summarize what you find.",
		Tools: []types.Tool{codeExecTool},
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
}

// handleCodeExecution dispatches a code execution tool call by inspecting the "type"
// discriminator and executing the appropriate operation locally.
func handleCodeExecution(ctx context.Context, rawInput map[string]interface{}) (interface{}, error) {
	// Marshal the input map back to JSON for type-safe unmarshaling
	inputJSON, err := json.Marshal(rawInput)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	input, err := anthropicTools.UnmarshalCodeExecutionInput(inputJSON)
	if err != nil {
		return nil, fmt.Errorf("parse code execution input: %w", err)
	}

	switch v := input.(type) {
	case *anthropicTools.BashCodeExecutionInput:
		return executeBash(ctx, v.Command)

	case *anthropicTools.ProgrammaticToolCallInput:
		// For this example, we simulate Python execution
		return simulatePythonExecution(v.Code)

	case *anthropicTools.TextEditorInput:
		return nil, fmt.Errorf("text editor operations not supported in this example")

	default:
		return nil, fmt.Errorf("unexpected input type: %T", input)
	}
}

// executeBash runs a shell command and returns the result.
func executeBash(ctx context.Context, command string) (*anthropicTools.BashExecutionResult, error) {
	fmt.Printf("[EXEC] bash: %s\n", command)

	// Run the command using /bin/sh
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start - return as error result
			return &anthropicTools.BashExecutionResult{
				Type:       anthropicTools.CodeExecutionResultTypeBash,
				Stdout:     stdout.String(),
				Stderr:     err.Error(),
				ReturnCode: 1,
			}, nil
		}
	}

	return &anthropicTools.BashExecutionResult{
		Type:       anthropicTools.CodeExecutionResultTypeBash,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ReturnCode: exitCode,
	}, nil
}

// simulatePythonExecution simulates Python code execution for demonstration purposes.
// In production, this would invoke a real Python interpreter.
func simulatePythonExecution(code string) (*anthropicTools.ProgrammaticExecutionResult, error) {
	fmt.Printf("[EXEC] python: %s\n", code)

	// For the example, just echo the code as a simulation
	return &anthropicTools.ProgrammaticExecutionResult{
		Type:       anthropicTools.CodeExecutionResultTypeProgrammatic,
		Stdout:     fmt.Sprintf("[simulated] executed: %s\n", code),
		Stderr:     "",
		ReturnCode: 0,
	}, nil
}
